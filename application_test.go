package appctl

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestApplication_Deadline(t *testing.T) {
	t.Parallel()
	var a Application
	dl, ok := a.Deadline()
	if ok {
		t.Error("application has deadline")
	}
	if dl != (time.Time{}) {
		t.Error("wrong result")
	}
}

func TestApplication_Done(t *testing.T) {
	// do not put t.Parallel
	var a = Application{
		holdOn: make(chan struct{}),
		done:   make(chan struct{}),
	}
	t.Run("test value", func(t *testing.T) {
		if a.Done() != a.done {
			t.Error("wrong value")
		}
	})
	t.Run("test chan state", func(t *testing.T) {
		select {
		case <-a.Done():
			t.Error("wrong channel state")
		default:
		}
	})
	t.Run("test chan close", func(t *testing.T) {
		close(a.done)
		select {
		case <-a.Done():
		default:
			t.Error("wrong channel state")
		}
	})
}

func TestApplication_Err(t *testing.T) {
	t.Parallel()
	type testCase struct {
		name    string
		app     Application
		needErr error
	}
	var testCases = []testCase{
		{
			name:    "on init state",
			app:     Application{appState: appStateInit},
			needErr: nil,
		},
		{
			name:    "on running state",
			app:     Application{appState: appStateRunning},
			needErr: nil,
		},
		{
			name:    "on holdon state",
			app:     Application{appState: appStateHoldOn},
			needErr: nil,
		},
		{
			name:    "on shutdown state",
			app:     Application{appState: appStateShutdown},
			needErr: ErrShutdown,
		},
	}
	for i := range testCases {
		test := testCases[i]
		t.Run(test.name, func(t *testing.T) {
			if e := test.app.Err(); e != test.needErr {
				t.Errorf("need: %v, got: %v", test.needErr, e)
			}
		})
	}
}

func TestApplication_HoldOn(t *testing.T) {
	t.Run("close channel", func(t *testing.T) {
		var a = Application{
			holdOn:   make(chan struct{}),
			done:     make(chan struct{}),
			appState: appStateRunning,
		}
		a.HoldOn()
		select {
		case <-a.done:
			t.Error("the done chan is closed")
		default:
			select {
			case <-a.holdOn:
				if a.appState != appStateHoldOn {
					t.Error("wrong app state")
				}
			default:
				t.Error("the holdOn chan is open")
			}
		}
	})
	t.Run("wrong state", func(t *testing.T) {
		var apps = []Application{
			{
				holdOn:   make(chan struct{}),
				done:     make(chan struct{}),
				appState: appStateInit,
			},
			{
				holdOn:   make(chan struct{}),
				done:     make(chan struct{}),
				appState: appStateHoldOn,
			},
			{
				holdOn:   make(chan struct{}),
				done:     make(chan struct{}),
				appState: appStateShutdown,
			},
		}
		for i := range apps {
			a := apps[i]
			a.HoldOn()
			select {
			case <-a.done:
				t.Error("the done chan is closed")
			default:
				select {
				case <-a.holdOn:
					t.Error("the holdOn chan is closed")
				default:
					// good case
				}
			}
		}
	})
}

func TestApplication_Run(t *testing.T) {
	t.Parallel()
	t.Run("Run stages", func(t *testing.T) {
		var a = Application{
			holdOn:   make(chan struct{}),
			done:     make(chan struct{}),
			appState: appStateInit,
			MainFunc: func(ctx context.Context, holdOn <-chan struct{}) error {
				return nil
			},
		}
		if err := a.Run(); err != nil {
			t.Error(err)
		}
	})
	t.Run("empty main", func(t *testing.T) {
		var a = Application{}
		if err := a.Run(); err != ErrMainOmitted {
			t.Errorf("want: %v, got: %v", ErrMainOmitted, err)
		}
	})
	t.Run("running twice", func(t *testing.T) {
		var result = false
		var a = Application{
			holdOn:   make(chan struct{}),
			done:     make(chan struct{}),
			appState: appStateRunning,
			MainFunc: func(ctx context.Context, holdOn <-chan struct{}) error {
				result = true
				return nil
			},
		}
		if err := a.Run(); err != ErrWrongState {
			t.Errorf("want: %v, got: %v", ErrWrongState, err)
		}
		if result {
			t.Error("wrong logic")
		}
	})
	t.Run("wrong init", func(t *testing.T) {
		var result = false
		var a = Application{
			holdOn:   make(chan struct{}),
			done:     make(chan struct{}),
			appState: appStateInit,
			Services: &ServiceKeeper{
				Services: []Service{
					&dummyService{brokeOnInit: true},
				},
			},
			MainFunc: func(ctx context.Context, holdOn <-chan struct{}) error {
				result = true
				return nil
			},
		}
		if err := a.Run(); err == nil {
			t.Errorf("want error, got: %v", err)
		}
		if result {
			t.Error("wrong logic")
		}
	})
	t.Run("break services", func(t *testing.T) {
		var a = Application{
			holdOn:   make(chan struct{}),
			done:     make(chan struct{}),
			appState: appStateInit,
			Services: &ServiceKeeper{
				Services: []Service{
					&dummyService{
						throttling:  time.Millisecond * 25,
						brokeOnPing: true,
					},
				},
				PingPeriod: time.Millisecond * 25,
			},
			MainFunc: func(ctx context.Context, holdOn <-chan struct{}) error {
				<-time.After(time.Second * 100)
				return nil
			},
		}
		if err := a.Run(); err == nil {
			t.Error("expected error here")
		}
	})
}

func TestApplication_Shutdown(t *testing.T) {
	t.Parallel()
	checkBothChannels := func(app Application) error {
		select {
		case <-app.holdOn:
			select {
			case <-app.done:
				// good case
				return nil
			default:
				return errors.New("not doned")
			}
		default:
			return errors.New("not holded on")
		}
	}
	type testCase struct {
		name      string
		app       Application
		needError error
		check     func(Application) error
	}
	var tests = []testCase{
		{
			name: "implicit shutdown",
			app: Application{
				MainFunc: func(ctx context.Context, holdOn <-chan struct{}) error {
					// exit with Shutdown automatic call
					return nil
				},
			},
			needError: nil,
			check:     checkBothChannels,
		},
		{
			name: "shutdown after holding on",
			app: Application{
				MainFunc: func(ctx context.Context, holdOn <-chan struct{}) error {
					go func() {
						<-time.After(time.Millisecond * 5)
						ctx.Value(AppContext{}).(*Application).HoldOn()
					}()
					<-holdOn
					return nil
				},
			},
			needError: nil,
			check:     checkBothChannels,
		},
		{
			name: "explicit shutdown",
			app: Application{
				MainFunc: func(ctx context.Context, holdOn <-chan struct{}) error {
					go func() {
						<-time.After(time.Millisecond * 5)
						ctx.Value(AppContext{}).(*Application).Shutdown()
					}()
					<-holdOn
					return nil
				},
			},
			needError: nil,
			check:     checkBothChannels,
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			a := test.app
			if err := a.Run(); err != test.needError {
				t.Errorf("want: %v, got: %v", test.needError, err)
			}
			if err := test.check(a); err != nil {
				t.Error(err)
			}
		})
	}
	t.Run("wrong state", func(t *testing.T) {
		var result = false
		var a = Application{
			holdOn:   make(chan struct{}),
			done:     make(chan struct{}),
			appState: appStateInit,
			MainFunc: func(ctx context.Context, holdOn <-chan struct{}) error {
				result = true
				<-holdOn
				t.Error(errors.New("test error"))
				return nil
			},
		}
		a.Shutdown()
		if result {
			t.Error("no any action expected")
		}
		select {
		case <-a.holdOn:
			t.Error("holdOn channel closed")
		case <-a.done:
			t.Error("done channel closed")
		default:
			// good case
		}
	})
	t.Run("holdOn state", func(t *testing.T) {
		var result = false
		var a = Application{
			holdOn:   make(chan struct{}),
			done:     make(chan struct{}),
			appState: appStateHoldOn,
			MainFunc: func(ctx context.Context, holdOn <-chan struct{}) error {
				result = true
				<-holdOn
				t.Error(errors.New("test error"))
				return nil
			},
		}
		a.Shutdown()
		if result {
			t.Error("no any action expected")
		}
		select {
		case <-a.holdOn:
			t.Error("holdOn channel closed")
		default:
			select {
			case <-a.done:
				// good case
			default:
				t.Error("done channel closed")
			}
		}
	})
}

func TestApplication_Value(t *testing.T) {
	t.Parallel()
	var a = Application{
		MainFunc: func(ctx context.Context, holdOn <-chan struct{}) error {
			if _, ok := ctx.Value(AppContext{}).(*Application); !ok {
				t.Error("wrong context")
			}
			return nil
		},
	}
	if err := a.Run(); err != nil {
		t.Error(err)
	}
}

func TestApplication_checkState(t *testing.T) {
	t.Parallel()
	type fields struct {
		appState int32
	}
	type args struct {
		old int32
		new int32
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name:   "0 -> 1",
			fields: fields{appState: 0},
			args:   args{old: 0, new: 1},
			want:   true,
		},
		{
			name:   "1 -> 3",
			fields: fields{appState: 1},
			args:   args{old: 1, new: 3},
			want:   true,
		},
		{
			name:   "4 -> 2",
			fields: fields{appState: 4},
			args:   args{old: 4, new: 2},
			want:   true,
		},
		{
			name:   "1 -> 1",
			fields: fields{appState: 1},
			args:   args{old: 1, new: 1},
			want:   true,
		},
		{
			name:   "err",
			fields: fields{appState: 5},
			args:   args{old: 1, new: 1},
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := Application{
				appState: tt.fields.appState,
			}
			if got := a.checkState(tt.args.old, tt.args.new); got != tt.want {
				t.Errorf("checkState() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplication_run(t *testing.T) {
	t.Parallel()
	t.Run("exit by SIGINT", func(t *testing.T) {
		var status int32
		var a = Application{
			appState: appStateRunning,
			holdOn:   make(chan struct{}),
			done:     make(chan struct{}),
			MainFunc: func(ctx context.Context, holdOn <-chan struct{}) error {
				<-holdOn
				atomic.CompareAndSwapInt32(&status, 1, 2)
				return nil
			},
			TerminationTimeout: time.Millisecond * 500,
		}
		var sig = make(chan os.Signal)
		go func() {
			<-time.After(time.Millisecond * 5)
			atomic.CompareAndSwapInt32(&status, 0, 1)
			sig <- syscall.SIGINT
		}()
		go func() {
			<-time.After(time.Millisecond * 100)
			select {
			case <-a.holdOn:
				return
			case <-a.done:
				return
			default:
				t.Error("timeout")
				close(a.holdOn)
				close(a.done)
			}
		}()
		if err := a.run(sig); err != nil {
			t.Error(err)
		}
		if atomic.LoadInt32(&status) != 2 {
			t.Error("unexpected exit")
		}
	})
	t.Run("exit by SIGINT and timeout", func(t *testing.T) {
		var status int32
		var a = Application{
			appState: appStateRunning,
			holdOn:   make(chan struct{}),
			done:     make(chan struct{}),
			MainFunc: func(ctx context.Context, holdOn <-chan struct{}) error {
				<-time.After(time.Second * 30)
				atomic.CompareAndSwapInt32(&status, 0, 2) // never happens
				return nil
			},
			TerminationTimeout: time.Millisecond * 10,
		}
		var sig = make(chan os.Signal)
		go func() {
			<-time.After(time.Millisecond * 5)
			sig <- syscall.SIGINT
		}()
		go func() {
			<-time.After(time.Millisecond * 500)
			select {
			case <-a.holdOn:
				return
			case <-a.done:
				return
			default:
				t.Error("test timeout")
				close(a.holdOn)
				close(a.done)
			}
		}()
		if err := a.run(sig); err != nil {
			t.Error(err)
		}
		if atomic.LoadInt32(&status) != 0 {
			t.Error("unexpected exit")
		}
	})
	t.Run("exit by main", func(t *testing.T) {
		var status int32
		var a = Application{
			appState: appStateRunning,
			holdOn:   make(chan struct{}),
			done:     make(chan struct{}),
			MainFunc: func(ctx context.Context, holdOn <-chan struct{}) error {
				atomic.CompareAndSwapInt32(&status, 0, 1)
				return nil
			},
			TerminationTimeout: time.Millisecond * 500,
		}
		var sig = make(chan os.Signal)
		go func() {
			<-time.After(time.Millisecond * 100)
			select {
			case <-a.done:
				return
			default:
				t.Error("timeout")
				close(a.holdOn)
				close(a.done)
			}
		}()
		if err := a.run(sig); err != nil {
			t.Error(err)
		}
		if atomic.LoadInt32(&status) != 1 {
			t.Error("unexpected exit")
		}
	})
	t.Run("exit by error", func(t *testing.T) {
		var status int32
		var a = Application{
			appState: appStateRunning,
			holdOn:   make(chan struct{}),
			done:     make(chan struct{}),
			MainFunc: func(ctx context.Context, holdOn <-chan struct{}) error {
				atomic.CompareAndSwapInt32(&status, 0, 1)
				return errors.New("error")
			},
			TerminationTimeout: time.Millisecond * 500,
		}
		var sig = make(chan os.Signal)
		go func() {
			<-time.After(time.Millisecond * 100)
			select {
			case <-a.done:
				return
			default:
				t.Error("timeout")
				close(a.holdOn)
				close(a.done)
			}
		}()
		if err := a.run(sig); err == nil {
			t.Error("expected error here")
		}
		if atomic.LoadInt32(&status) != 1 {
			t.Error("unexpected exit")
		}
	})
	t.Run("exit by panic", func(t *testing.T) {
		var status int32
		var a = Application{
			appState: appStateRunning,
			holdOn:   make(chan struct{}),
			done:     make(chan struct{}),
			MainFunc: func(ctx context.Context, holdOn <-chan struct{}) error {
				atomic.CompareAndSwapInt32(&status, 0, 1)
				panic(errors.New("error"))
			},
			TerminationTimeout: time.Millisecond * 500,
		}
		var sig = make(chan os.Signal)
		go func() {
			<-time.After(time.Millisecond * 100)
			select {
			case <-a.done:
				return
			default:
				t.Error("timeout")
				close(a.holdOn)
				close(a.done)
			}
		}()
		if err := a.run(sig); err == nil {
			t.Error("expected error here")
		}
		if atomic.LoadInt32(&status) != 1 {
			t.Error("unexpected exit")
		}
	})
}

func TestApplication_setError(t *testing.T) {
	t.Parallel()
	t.Run("normal setError", func(t *testing.T) {
		var e = errors.New("test 1")
		var a = Application{
			appState: appStateRunning,
			holdOn:   make(chan struct{}),
			done:     make(chan struct{}),
		}
		a.setError(e)
		if a.err != e {
			t.Errorf("expected: %v, got: %v", e, a.err)
		}
	})
	t.Run("nil setError", func(t *testing.T) {
		var e error
		var a = Application{
			appState: appStateRunning,
			holdOn:   make(chan struct{}),
			done:     make(chan struct{}),
		}
		a.setError(e)
		if a.err != nil {
			t.Errorf("expected: %v, got: %v", e, a.err)
		}
	})
	t.Run("concurrent setError", func(t *testing.T) {
		var e = errors.New("test error 2")
		var a = Application{
			appState: appStateRunning,
			holdOn:   make(chan struct{}),
			done:     make(chan struct{}),
		}
		var wg sync.WaitGroup
		wg.Add(1000)
		for nn := 0; nn < 1000; nn++ {
			go func(i int) {
				if i == 50 {
					a.setError(e)
				} else {
					if a.checkState(appStateRunning, appStateRunning) {
						a.setError(nil)
					} else {
						a.setError(errors.New("test error 4"))
					}
				}
				wg.Done()
			}(nn)
		}
		wg.Wait()
		if a.err != e {
			t.Errorf("expected: %v, got: %v", e, a.err)
		}
	})
}

func TestApplication_init(t *testing.T) {
	t.Parallel()
	t.Run("init timeout", func(t *testing.T) {
		var a = Application{
			appState: appStateRunning,
			Services: &ServiceKeeper{
				Services: []Service{
					&dummyService{throttling: time.Second},
				},
			},
			InitializationTimeout: time.Millisecond * 2,
		}
		if err := a.init(); err == nil {
			t.Error("expected timeout error here")
		}
	})
	t.Run("defaults", func(t *testing.T) {
		var a = Application{
			appState: appStateRunning,
		}
		if err := a.init(); err != nil {
			t.Error(err)
		}
		if a.InitializationTimeout != defaultInitializationTimeout || a.TerminationTimeout != defaultTerminationTimeout {
			t.Error("incorrect default values")
		}
		if a.done == nil || a.holdOn == nil {
			t.Error("channels is not initialized")
		}
	})
}
