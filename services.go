package appctl

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type (
	Service interface {
		// Init tries to perform the initial initialization of the service, the logic of the function must make sure
		// that all created connections to remote services are in working order and are pinging. Otherwise, the
		// application will need additional error handling.
		Init(ctx context.Context) error
		// Ping will be called by the service controller at regular intervals, it is important that a response with
		// any error will be regarded as an unrecoverable state of the service and will lead to an emergency stop of
		// the application. If the service is not critical for the application, like a memcached, then try to implement
		// the logic of self-diagnosis and service recovery inside Ping, and return the nil as a response even if the
		// recovery failed.
		Ping(ctx context.Context) error
		// Close will be executed when the service controller receives a stop command. Normally, this happens after the
		// main thread of the application has already finished. That is, no more requests from the outside are expected.
		Close() error
		// Ident identifies a particular service to display the sources of failures in the error logs
		Ident() string
	}
	ServiceKeeper struct {
		// Services contains a slice of services that need to be watched.
		Services []Service
		// PingPeriod allows you to configure the interval for polling for health. Default = 15 second.
		PingPeriod time.Duration
		// PingTimeout limits the time for which the service must respond to Ping execution. Default = 5 second.
		PingTimeout time.Duration
		// ShutdownTimeout limits the time in which all services must have time to release resources. Default = minute.
		ShutdownTimeout time.Duration
		// SyncStopWatch synchronizes the Stop and Watch methods. If this parameter is set to `true`,
		// the resource release process is started only after Watch finishes its work.
		SyncStopWatch bool

		// DetectedProblem intercepts errors that have occurred and can silence them in some cases.
		DetectedProblem func(error) error
		// Recovered indicates that the problem that occurred earlier has been resolved.
		// This means that repeated pings no longer cause errors.
		Recovered func() error

		stop  chan struct{}
		done  chan struct{}
		state int32
		mux   sync.Mutex
		err   error
	}
)

const (
	srvStateInit int32 = iota
	srvStateReady
	srvStateRunning
	srvStateShutdown
	srvStateOff

	defaultPingPeriod      = 15 * time.Second
	defaultPingTimeout     = 5 * time.Second
	defaultShutdownTimeout = time.Minute
)

// Init will initialize all registered services. Will return an error if at least one of the initialization functions
// returned an error. It is very important that after the first error, the context with which the initialization
// functions of all services are performed will be immediately canceled.
func (s *ServiceKeeper) Init(ctx context.Context) error {
	if !s.checkState(srvStateInit, srvStateReady) {
		return ErrWrongState
	}
	if err := s.initAllServices(ctx); err != nil {
		return err
	}
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	s.defaultConfigs()
	return nil
}

func (s *ServiceKeeper) defaultConfigs() {
	if s.PingPeriod == 0 {
		s.PingPeriod = defaultPingPeriod
	}
	if s.PingTimeout == 0 {
		s.PingTimeout = defaultPingTimeout
	}
	if s.ShutdownTimeout == 0 {
		s.ShutdownTimeout = defaultShutdownTimeout
	}
	if s.DetectedProblem == nil {
		s.DetectedProblem = func(err error) error {
			return err
		}
	}
	if s.Recovered == nil {
		s.Recovered = func() error {
			return nil
		}
	}
}

func (s *ServiceKeeper) checkState(old, new int32) bool {
	return atomic.CompareAndSwapInt32(&s.state, old, new)
}

func (s *ServiceKeeper) initAllServices(ctx context.Context) (initError error) {
	for i := range s.Services {
		select {
		case <-ctx.Done():
			return ctx.Err()

		default:
			err := s.Services[i].Init(ctx)
			if err != nil {
				return fmt.Errorf("error while %q initialization: %w", s.Services[i].Ident(), err)
			}
		}
	}
	return nil
}

func (s *ServiceKeeper) testServices(ctx context.Context) error {
	var ctxPing, cancel = context.WithTimeout(ctx, s.PingTimeout)
	defer cancel()
	var p parallelRun
	for i := range s.Services {
		p.do(ctxPing, s.Services[i].Ident(), s.Services[i].Ping)
	}
	return p.wait()
}

// Watch monitors the health of resources. At a given frequency, all services will receive a Ping command,
// and if any of the responses contains an error, all execution will immediately stop and the error will be
// transmitted as a result of the Watch procedure.
//
// This procedure is synchronous, which means that control of the routine will be returned only when service monitoring is interrupted.
func (s *ServiceKeeper) Watch(ctx context.Context) error {
	defer close(s.done)
	if !s.checkState(srvStateReady, srvStateRunning) {
		return ErrWrongState
	}
	if err := s.cycleTestServices(ctx); err != nil && !errors.Is(err, ErrShutdown) {
		return err
	}
	return nil
}

func (s *ServiceKeeper) cycleTestServices(ctx context.Context) error {
	var ps = pingState{
		keeper:     s,
		errorState: false,
	}
	var cs = time.NewTicker(s.PingPeriod)
	defer cs.Stop()
	for {
		select {

		case <-s.stop:
			return nil

		case <-cs.C:
			withTimeout, cancel := context.WithTimeout(ctx, s.PingPeriod)
			if err := ps.ping(withTimeout); err != nil {
				cancel()
				return err
			}
			cancel()

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *ServiceKeeper) setErr(err error) {
	if err == nil {
		return
	}
	if errors.Is(err, ErrShutdown) {
		return
	}
	s.mux.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mux.Unlock()
}

type pingState struct {
	keeper     *ServiceKeeper
	errorState bool
}

func (p *pingState) ping(ctx context.Context) error {
	err := p.keeper.testServices(ctx)
	switch {

	case err != nil:
		p.errorState = true
		return p.keeper.detectedProblem(err) // intercept problem

	case err == nil && p.errorState:
		p.errorState = false
		return p.keeper.recovered() // intercept recover state

	default:
		return err
	}
}

func (s *ServiceKeeper) detectedProblem(err error) error {
	if s.DetectedProblem == nil {
		return err
	}
	return s.DetectedProblem(err)
}

func (s *ServiceKeeper) recovered() error {
	if s.Recovered == nil {
		return nil
	}
	return s.Recovered()
}

// Stop sends a signal that monitoring should be stopped. Stops execution of the Watch procedure
func (s *ServiceKeeper) Stop() {
	if s.checkState(srvStateRunning, srvStateShutdown) {
		close(s.stop)
	}
	if s.SyncStopWatch {
		<-s.done
	}
}

func (s *ServiceKeeper) Release() error {
	if s.checkState(srvStateShutdown, srvStateOff) {
		return s.release()
	}
	return ErrWrongState
}

func (s *ServiceKeeper) release() error {
	var errs arrError
	var done = make(chan struct{})
	go func() {
		defer close(done)
		for i := len(s.Services) - 1; i >= 0; i-- {
			if err := s.Services[i].Close(); err != nil {
				errs = append(errs, fmt.Errorf("error while stopping %q service: %w", s.Services[i].Ident(), err))
			}
		}
	}()
	select {
	case <-done:
		if len(errs) > 0 {
			return errs
		}
		return nil

	case <-time.After(s.ShutdownTimeout):
		return fmt.Errorf("shutdown timeout exceeded: %v", s.ShutdownTimeout)
	}
}
