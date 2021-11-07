package appctl

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type (
	// Resources is an abstraction representing application resources.
	Resources interface {
		// Init is executed before transferring control to MainFunc. Should initialize resources and check their
		// minimum health. If an error is returned, MainFunc will not be started.
		Init(context.Context) error
		// Watch is executed in the background, monitors the state of resources.
		// Exiting this procedure will immediately stop the application.
		Watch(context.Context) error
		// Stop signals the Watch procedure to terminate the work
		Stop()
		// Release releases the resources. Executed just before exiting the Application.Run
		Release() error
	}
	Application struct {
		// MainFunc will run as the main thread of execution when you execute the Run method.
		// Termination of this function will result in the termination of Run, the error that was passed as a
		// result will be thrown as a result of Run execution.
		//
		// The holdOn channel controls the runtime of the application, as soon as it closes, you need to gracefully
		// complete all current tasks and exit the MainFunc.
		MainFunc func(ctx context.Context, holdOn <-chan struct{}) error
		// Resources is an abstraction that represents the resources needed to execute the main thread.
		// The health of resources directly affects the main thread of execution.
		Resources Resources
		// TerminationTimeout limits the time for the main thread to terminate. On normal shutdown,
		// if MainFunc does not return within the allotted time, the job will terminate with an ErrTermTimeout error.
		TerminationTimeout time.Duration
		// InitializationTimeout limits the time to initialize resources.
		// If the resources are not initialized within the allotted time, the application will not be launched
		InitializationTimeout time.Duration

		appState int32
		mux      sync.Mutex
		err      error
		holdOn   chan struct{}
		done     chan struct{}
	}
	AppContext struct{}
)

const (
	appStateInit int32 = iota
	appStateRunning
	appStateHoldOn
	appStateShutdown
)

const (
	defaultTerminationTimeout    = time.Second
	defaultInitializationTimeout = time.Second * 15
)

func (a *Application) init() error {
	if a.TerminationTimeout == 0 {
		a.TerminationTimeout = defaultTerminationTimeout
	}
	if a.InitializationTimeout == 0 {
		a.InitializationTimeout = defaultInitializationTimeout
	}
	a.holdOn = make(chan struct{})
	a.done = make(chan struct{})
	if a.Resources != nil {
		ctx, cancel := context.WithTimeout(a, a.InitializationTimeout)
		defer cancel()
		return a.Resources.Init(ctx)
	}
	return nil
}

func (a *Application) run(sig <-chan os.Signal) error {
	defer a.Shutdown()
	var errRun = make(chan error, 1)
	go func() {
		defer close(errRun)
		if err := a.MainFunc(a, a.holdOn); err != nil {
			errRun <- err
		}
	}()
	var errHld = make(chan error, 1)
	go func() {
		defer close(errHld)
		select {
		// wait for os signal
		case <-sig:
			a.HoldOn()
			// In this mode, the main thread should stop accepting new requests, terminate all current requests, and exit.
			// Exiting the procedure of the main thread will lead to an implicit call Shutdown(),
			// if this does not happen, we will make an explicit call through the shutdown timeout
			select {
			case <-time.After(a.TerminationTimeout):
				errHld <- ErrTermTimeout
			case <-a.done:
				// ok
			}
		// if shutdown
		case <-a.done:
			// exit immediately
		}
	}()
	select {
	case err, ok := <-errRun:
		if ok && err != nil {
			return err
		}
	case err, ok := <-errHld:
		if ok && err != nil {
			return err
		}
	case <-a.done:
		// shutdown
	}
	return nil
}

func (a *Application) checkState(old, new int32) bool {
	return atomic.CompareAndSwapInt32(&a.appState, old, new)
}

func (a *Application) setError(err error) {
	if err == nil {
		return
	}
	a.mux.Lock()
	if a.err == nil {
		a.err = err
	}
	a.mux.Unlock()
	a.Shutdown()
}

func (a *Application) getError() error {
	var err error
	a.mux.Lock()
	err = a.err
	a.mux.Unlock()
	return err
}

// Run starts the execution of the main application thread with the MainFunc function.
// Returns an error if the execution of the application ended abnormally, otherwise it will return a nil.
func (a *Application) Run() error {
	if a.MainFunc == nil {
		return ErrMainOmitted
	}
	if a.checkState(appStateInit, appStateRunning) {
		if err := a.init(); err != nil {
			a.err = err
			a.appState = appStateShutdown
			return err
		}
		var servicesRunning = make(chan struct{})
		if a.Resources != nil {
			go func() {
				defer close(servicesRunning)
				// if the stop is due to the correct stop of services without any error,
				// we still have to stop the application
				defer a.Shutdown()
				a.setError(a.Resources.Watch(a))
			}()
		}
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
		// main thread execution
		a.setError(a.run(sig))
		// shutdown
		if a.Resources != nil {
			a.Resources.Stop()
			select {
			case <-servicesRunning:
			case <-time.After(a.TerminationTimeout):
			}
			a.setError(a.Resources.Release())
		}
		return a.getError()
	}
	return ErrWrongState
}

// HoldOn signals the application to terminate the current computational processes and prepare to stop the application.
func (a *Application) HoldOn() {
	if a.checkState(appStateRunning, appStateHoldOn) {
		close(a.holdOn)
	}
}

// Shutdown stops the application immediately. At this point, all calculations should be completed.
func (a *Application) Shutdown() {
	a.HoldOn()
	if a.checkState(appStateHoldOn, appStateShutdown) {
		close(a.done)
	}
}

// Deadline returns the time when work done on behalf of this context should be canceled.
func (a *Application) Deadline() (deadline time.Time, ok bool) {
	return time.Time{}, false
}

// Done returns a channel that's closed when work done on behalf of this context should be canceled.
func (a *Application) Done() <-chan struct{} {
	return a.done
}

// Err returns error when application is closed.
// If Done is not yet closed, Err returns nil. If Done is closed, Err returns ErrShutdown.
func (a *Application) Err() error {
	if err := a.getError(); err != nil {
		return err
	}
	if atomic.LoadInt32(&a.appState) == appStateShutdown {
		return ErrShutdown
	}
	return nil
}

// Value returns the Application object.
func (a *Application) Value(key interface{}) interface{} {
	var appContext = AppContext{}
	if key == appContext {
		return a
	}
	return nil
}
