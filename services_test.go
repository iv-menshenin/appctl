package appctl

import (
	"context"
	"errors"
	"testing"
	"time"
)

type (
	dummyService struct {
		brokeOnInit  bool
		brokeOnPing  bool
		brokeOnClose bool
		passedInit   bool
		passedPing   bool
		passedClose  bool
		throttling   time.Duration
	}
)

func (d *dummyService) Init(ctx context.Context) error {
	if d.brokeOnInit {
		return errors.New("bang")
	}
	if d.throttling > 0 {
		<-time.After(d.throttling)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		d.passedInit = true
		return nil
	}
}

func (d *dummyService) Ping(ctx context.Context) error {
	if d.brokeOnPing {
		return errors.New("bang")
	}
	if d.throttling > 0 {
		<-time.After(d.throttling)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		d.passedPing = true
		return nil
	}
}

func (d *dummyService) Close() error {
	if d.brokeOnClose {
		return errors.New("bang")
	}
	if d.throttling > 0 {
		<-time.After(d.throttling)
	}
	d.passedClose = true
	return nil
}

func TestServiceKeeper_Init(t *testing.T) {
	t.Parallel()
	type fields struct {
		Services        []Service
		PingPeriod      time.Duration
		PingTimeout     time.Duration
		ShutdownTimeout time.Duration
		stop            chan struct{}
		state           int32
	}
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
		check   func(*ServiceKeeper) error
	}{
		{
			name: "wrong state",
			fields: fields{
				state: appStateReady,
			},
			args:    args{ctx: context.TODO()},
			wantErr: true,
		},
		{
			name: "with dummy services and default configs",
			fields: fields{
				Services: []Service{
					&dummyService{brokeOnClose: true, brokeOnPing: true},
					&dummyService{brokeOnClose: true, brokeOnPing: true},
				},
				state: appStateInit,
			},
			args:    args{ctx: context.TODO()},
			wantErr: false,
			check: func(c *ServiceKeeper) error {
				if !c.Services[0].(*dummyService).passedInit {
					return errors.New("first service was not initialized")
				}
				if !c.Services[1].(*dummyService).passedInit {
					return errors.New("second service was not initialized")
				}
				if c.PingPeriod != defaultPingPeriod || c.PingTimeout != defaultPingTimeout || c.ShutdownTimeout != defaultShutdownTimeout {
					return errors.New("wrong timing config")
				}
				return nil
			},
		},
		{
			name: "with dummy services",
			fields: fields{
				Services: []Service{
					&dummyService{brokeOnClose: true, brokeOnPing: true},
					&dummyService{brokeOnClose: true, brokeOnPing: true},
				},
				state:           appStateInit,
				PingTimeout:     time.Second * 100,
				PingPeriod:      time.Second * 200,
				ShutdownTimeout: time.Second * 300,
			},
			args:    args{ctx: context.TODO()},
			wantErr: false,
			check: func(c *ServiceKeeper) error {
				if !c.Services[0].(*dummyService).passedInit {
					return errors.New("first service was not initialized")
				}
				if !c.Services[1].(*dummyService).passedInit {
					return errors.New("second service was not initialized")
				}
				if c.PingPeriod != time.Second*200 || c.PingTimeout != time.Second*100 {
					return errors.New("wrong timing config")
				}
				if c.ShutdownTimeout != time.Second*300 {
					return errors.New("wrong timing config")
				}
				return nil
			},
		},
		{
			name: "with broken services",
			fields: fields{
				Services: []Service{
					&dummyService{},
					&dummyService{brokeOnInit: true},
				},
				state: appStateInit,
			},
			args:    args{ctx: context.TODO()},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ServiceKeeper{
				Services:        tt.fields.Services,
				PingPeriod:      tt.fields.PingPeriod,
				PingTimeout:     tt.fields.PingTimeout,
				ShutdownTimeout: tt.fields.ShutdownTimeout,
				stop:            tt.fields.stop,
				state:           tt.fields.state,
			}
			if err := s.Init(tt.args.ctx); (err != nil) != tt.wantErr {
				t.Errorf("Init() error = %v, wantErr %v", err, tt.wantErr)
			} else if err == nil {
				if err = tt.check(s); err != nil {
					t.Error(err)
				}
			}
		})
	}
}

func TestServiceKeeper_initAllServices(t *testing.T) {
	t.Parallel()
	type fields struct {
		Services []Service
	}
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name:    "empty",
			fields:  fields{},
			args:    args{ctx: context.TODO()},
			wantErr: false,
		},
		{
			name: "with dummy services",
			fields: fields{
				Services: []Service{
					&dummyService{brokeOnClose: true, brokeOnPing: true},
					&dummyService{brokeOnClose: true, brokeOnPing: true},
				},
			},
			args:    args{ctx: context.TODO()},
			wantErr: false,
		},
		{
			name: "with broken service",
			fields: fields{
				Services: []Service{
					&dummyService{},
					&dummyService{brokeOnInit: true},
				},
			},
			args:    args{ctx: context.TODO()},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ServiceKeeper{
				Services: tt.fields.Services,
			}
			if err := s.Init(tt.args.ctx); (err != nil) != tt.wantErr {
				t.Errorf("Init() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestServiceKeeper_Stop(t *testing.T) {
	t.Run("channel closed", func(t *testing.T) {
		t.Parallel()
		s := &ServiceKeeper{
			stop:  make(chan struct{}),
			state: appStateRunning,
		}
		s.Stop()
		select {
		case <-s.stop:
			// ok
		default:
			t.Error("channel not closed")
		}
	})
	t.Run("channel not closed", func(t *testing.T) {
		t.Parallel()
		s := &ServiceKeeper{
			stop:  make(chan struct{}),
			state: appStateHoldOn,
		}
		s.Stop()
		select {
		case <-s.stop:
			t.Error("channel closed")
		default:
			// ok
		}
	})
}

func TestServiceKeeper_Watch(t *testing.T) {
	t.Parallel()
	makeShortContext := func() context.Context {
		ctx, _ := context.WithTimeout(context.Background(), time.Millisecond*50)
		return ctx
	}
	type fields struct {
		Services    []Service
		PingPeriod  time.Duration
		PingTimeout time.Duration
		stop        chan struct{}
		state       int32
	}
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name     string
		fields   fields
		args     args
		parallel func(*ServiceKeeper)
		wantErr  bool
	}{
		{
			name: "test watch with wrong state",
			fields: fields{
				Services: []Service{
					&dummyService{},
				},
				stop:  make(chan struct{}),
				state: appStateRunning,
			},
			args:    args{ctx: context.TODO()},
			wantErr: true,
		},
		{
			name: "by error in ping",
			fields: fields{
				Services: []Service{
					&dummyService{brokeOnPing: true},
				},
				stop:  make(chan struct{}),
				state: appStateReady,
			},
			args:    args{ctx: context.TODO()},
			wantErr: true,
		},
		{
			name: "test watch with closing",
			fields: fields{
				Services: []Service{
					&dummyService{},
				},
				PingPeriod:  time.Millisecond * 10,
				PingTimeout: time.Millisecond * 100,
				stop:        make(chan struct{}),
				state:       appStateReady,
			},
			args: args{ctx: context.TODO()},
			parallel: func(s *ServiceKeeper) {
				<-time.After(time.Millisecond * 5)
				s.Stop()
			},
			wantErr: false,
		},
		{
			name: "test watch with context cancellation",
			fields: fields{
				Services: []Service{
					&dummyService{},
				},
				PingPeriod:  time.Millisecond * 10,
				PingTimeout: time.Millisecond * 100,
				stop:        make(chan struct{}),
				state:       appStateReady,
			},
			args: args{ctx: makeShortContext()},
			parallel: func(s *ServiceKeeper) {
				<-time.After(time.Millisecond * 5000)
				s.Stop()
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ServiceKeeper{
				Services:    tt.fields.Services,
				PingPeriod:  tt.fields.PingPeriod,
				PingTimeout: tt.fields.PingTimeout,
				stop:        tt.fields.stop,
				state:       tt.fields.state,
			}
			if tt.parallel != nil {
				go tt.parallel(s)
			}
			if err := s.Watch(tt.args.ctx); (err != nil) != tt.wantErr {
				t.Errorf("Watch() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestServiceKeeper_checkState(t *testing.T) {
	t.Parallel()
	type fields struct {
		Services    []Service
		PingPeriod  time.Duration
		PingTimeout time.Duration
		stop        chan struct{}
		state       int32
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
			name: "true",
			fields: fields{
				state: 1,
			},
			args: args{
				old: 1,
				new: 2,
			},
			want: true,
		},
		{
			name: "false",
			fields: fields{
				state: 1,
			},
			args: args{
				old: 2,
				new: 3,
			},
			want: false,
		},
		{
			name: "zero",
			fields: fields{
				state: 0,
			},
			args: args{
				old: 0,
				new: 0,
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ServiceKeeper{
				Services:    tt.fields.Services,
				PingPeriod:  tt.fields.PingPeriod,
				PingTimeout: tt.fields.PingTimeout,
				stop:        tt.fields.stop,
				state:       tt.fields.state,
			}
			if got := s.checkState(tt.args.old, tt.args.new); got != tt.want {
				t.Errorf("checkState() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServiceKeeper_cycleTestServices(t *testing.T) {
	t.Run("broken ping", func(t *testing.T) {
		t.Parallel()
		s := &ServiceKeeper{
			Services: []Service{
				&dummyService{brokeOnPing: true},
			},
			PingPeriod:  time.Millisecond,
			PingTimeout: time.Millisecond * 5,
			stop:        make(chan struct{}),
			state:       appStateRunning,
		}
		err := s.cycleTestServices(context.Background())
		if err == nil {
			t.Error("want error")
		}
	})
	t.Run("context cancellation", func(t *testing.T) {
		t.Parallel()
		s := &ServiceKeeper{
			Services:    []Service{},
			PingPeriod:  time.Millisecond,
			PingTimeout: time.Millisecond * 5,
			stop:        make(chan struct{}),
			state:       appStateRunning,
		}
		ctx, cancel := context.WithCancel(context.Background())
		var errCh = make(chan error)
		go func(ctx context.Context) {
			defer close(errCh)
			err := s.cycleTestServices(ctx)
			if err != nil {
				errCh <- err
			}
		}(ctx)
		select {
		case err, ok := <-errCh:
			if ok {
				t.Error(err)
			} else {
				t.Error("some unexpected")
			}
			cancel()
		default:
			cancel()
			<-time.After(time.Millisecond * 10)
		}
		err, ok := <-errCh
		if !ok || err != context.Canceled {
			t.Error("error expected")
		}
	})
	t.Run("service stopped", func(t *testing.T) {
		t.Parallel()
		s := &ServiceKeeper{
			Services:    []Service{},
			PingPeriod:  time.Millisecond,
			PingTimeout: time.Millisecond * 5,
			stop:        make(chan struct{}),
			state:       appStateRunning,
		}
		var errCh = make(chan error)
		go func() {
			defer close(errCh)
			err := s.cycleTestServices(context.Background())
			if err != nil {
				errCh <- err
			}
		}()
		select {
		case err, ok := <-errCh:
			if ok {
				t.Error(err)
			} else {
				t.Error("some unexpected")
			}
			s.Stop()
		default:
			<-time.After(time.Millisecond * 10)
			s.Stop()
		}
		err, ok := <-errCh
		if ok || err != nil {
			t.Error("error expected")
		}
	})
}

func TestServiceKeeper_testServices(t *testing.T) {
	t.Parallel()
	type fields struct {
		Services    []Service
		PingTimeout time.Duration
	}
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "broken ping",
			fields: fields{
				Services: []Service{
					&dummyService{brokeOnPing: true},
				},
				PingTimeout: time.Millisecond * 5,
			},
			args:    args{ctx: context.TODO()},
			wantErr: true,
		},
		{
			name: "broken ping 2",
			fields: fields{
				Services: []Service{
					&dummyService{brokeOnPing: false},
					&dummyService{brokeOnPing: false},
					&dummyService{brokeOnPing: true},
					&dummyService{brokeOnPing: false},
				},
				PingTimeout: time.Millisecond * 5,
			},
			args:    args{ctx: context.TODO()},
			wantErr: true,
		},
		{
			name: "all is ok",
			fields: fields{
				Services: []Service{
					&dummyService{},
					&dummyService{},
					&dummyService{},
					&dummyService{},
				},
				PingTimeout: time.Millisecond * 5,
			},
			args:    args{ctx: context.TODO()},
			wantErr: false,
		},
		{
			name: "throttle",
			fields: fields{
				Services: []Service{
					&dummyService{
						throttling: time.Millisecond * 10,
					},
					&dummyService{},
					&dummyService{
						throttling: time.Millisecond * 10,
					},
					&dummyService{},
				},
				PingTimeout: time.Millisecond * 1,
			},
			args:    args{ctx: context.TODO()},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ServiceKeeper{
				Services:    tt.fields.Services,
				PingTimeout: tt.fields.PingTimeout,
				stop:        make(chan struct{}),
			}
			if err := s.testServices(tt.args.ctx); (err != nil) != tt.wantErr {
				t.Errorf("cycleTestServices() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestServiceKeeper_Release(t *testing.T) {
	t.Run("wrong state", func(t *testing.T) {
		t.Parallel()
		s := &ServiceKeeper{
			state: appStateReady,
		}
		if err := s.Release(); err != ErrWrongState {
			t.Errorf("expected wrong state, got: %v", err)
		}
	})
	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		s := &ServiceKeeper{
			state:           appStateShutdown,
			ShutdownTimeout: time.Second,
		}
		if err := s.Release(); err != nil {
			t.Errorf("got error: %v", err)
		}
	})
}

func TestServiceKeeper_release(t *testing.T) {
	t.Parallel()
	type fields struct {
		Services        []Service
		ShutdownTimeout time.Duration
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "normal closing",
			fields: fields{
				Services: []Service{
					&dummyService{throttling: time.Microsecond * 15},
					&dummyService{throttling: time.Microsecond * 5},
					&dummyService{},
				},
				ShutdownTimeout: time.Millisecond * 10,
			},
			wantErr: false,
		},
		{
			name: "timeout",
			fields: fields{
				Services: []Service{
					&dummyService{throttling: time.Second},
				},
				ShutdownTimeout: time.Millisecond * 10,
			},
			wantErr: true,
		},
		{
			name: "error",
			fields: fields{
				Services: []Service{
					&dummyService{brokeOnClose: true},
					&dummyService{throttling: time.Microsecond * 12},
				},
				ShutdownTimeout: time.Second * 10,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ServiceKeeper{
				Services:        tt.fields.Services,
				ShutdownTimeout: tt.fields.ShutdownTimeout,
			}
			if err := s.release(); (err != nil) != tt.wantErr {
				t.Errorf("release() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
