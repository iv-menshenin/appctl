package appctl

import (
	"context"
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
		Services        []Service
		PingPeriod      time.Duration
		PingTimeout     time.Duration
		ShutdownTimeout time.Duration

		DetectedProblem func(error) error
		Recovered       func() error

		stop  chan struct{}
		state int32
	}
)

const (
	srvStateInit int32 = iota
	srvStateReady
	srvStateRunning
	srvStateShutdown
	srvStateOff

	defaultPingPeriod      = time.Second * 5
	defaultPingTimeout     = time.Millisecond * 1500
	defaultShutdownTimeout = time.Millisecond * 15000
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
	initCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var p parallelRun
	for i := range s.Services {
		p.do(initCtx, s.Services[i].Ident(), s.Services[i].Init)
	}
	return p.wait()
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
	if !s.checkState(srvStateReady, srvStateRunning) {
		return ErrWrongState
	}
	if err := s.cycleTestServices(ctx); err != nil && err != ErrShutdown {
		return s.detectedProblem(err)
	}
	return s.recovered()
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

func (s *ServiceKeeper) cycleTestServices(ctx context.Context) error {
	for {
		select {
		case <-s.stop:
			return nil
		case <-time.After(s.PingPeriod):
			if err := s.testServices(ctx); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Stop sends a signal that monitoring should be stopped. Stops execution of the Watch procedure
func (s *ServiceKeeper) Stop() {
	if s.checkState(srvStateRunning, srvStateShutdown) {
		close(s.stop)
	}
}

func (s *ServiceKeeper) Release() error {
	if s.checkState(srvStateShutdown, srvStateOff) {
		return s.release()
	}
	return ErrWrongState
}

func (s *ServiceKeeper) release() error {
	shCtx, cancel := context.WithTimeout(context.Background(), s.ShutdownTimeout)
	defer cancel()
	var p parallelRun
	for i := range s.Services {
		var service = s.Services[i]
		p.do(shCtx, service.Ident(), func(_ context.Context) error {
			return service.Close()
		})
	}
	var errCh = make(chan error)
	go func() {
		defer close(errCh)
		if err := p.wait(); err != nil {
			errCh <- err
		}
	}()
	for {
		select {
		case err, ok := <-errCh:
			if ok {
				return err
			}
			return nil
		case <-shCtx.Done():
			return shCtx.Err()
		}
	}
}
