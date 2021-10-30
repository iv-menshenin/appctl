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
	}
	ServiceController struct {
		Services    []Service
		PingPeriod  time.Duration
		PingTimeout time.Duration

		stop  chan struct{}
		state int32
	}
)

func (s *ServiceController) checkState(old, new int32) bool {
	return atomic.CompareAndSwapInt32(&s.state, old, new)
}

func (s *ServiceController) initAllServices(ctx context.Context) (initError error) {
	initCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var p parallelRun
	for i := range s.Services {
		p.Do(initCtx, s.Services[i].Init)
	}
	return p.Wait()
}

const (
	defaultPingPeriod  = time.Second * 5
	defaultPingTimeout = time.Millisecond * 1500
)

// Init will initialize all registered services. Will return an error if at least one of the initialization functions
// returned an error. It is very important that after the first error, the context with which the initialization
// functions of all services are performed will be immediately canceled.
func (s *ServiceController) Init(ctx context.Context) error {
	if !s.checkState(appStateInit, appStateReady) {
		return ErrWrongState
	}
	if err := s.initAllServices(ctx); err != nil {
		return err
	}
	s.stop = make(chan struct{})
	if s.PingPeriod == 0 {
		s.PingPeriod = defaultPingPeriod
	}
	if s.PingTimeout == 0 {
		s.PingTimeout = defaultPingTimeout
	}
	return nil
}

func (s *ServiceController) testServices(ctx context.Context) (testError error) {
	var ctxPing, cancel = context.WithTimeout(ctx, s.PingTimeout)
	defer cancel()
	var p parallelRun
	for i := range s.Services {
		p.Do(ctxPing, s.Services[i].Ping)
	}
	return p.Wait()
}

func (s *ServiceController) cycleTestServices(ctx context.Context) error {
	for {
		select {
		case <-s.stop:
			return nil
		case <-time.After(s.PingPeriod):
			if err := s.testServices(ctx); err != nil {
				return err
			}
		}
	}
}

func (s *ServiceController) Watch(ctx context.Context) error {
	if !s.checkState(appStateReady, appStateRunning) {
		return ErrWrongState
	}
	return s.cycleTestServices(ctx)
}

func (s *ServiceController) Stop() {
	if s.checkState(appStateRunning, appStateShutdown) {
		close(s.stop)
	}
}
