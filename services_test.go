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
	}
)

func (d *dummyService) Init(ctx context.Context) error {
	if d.brokeOnInit {
		return errors.New("bang")
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
	d.passedClose = true
	return nil
}

func TestServiceController_Init(t *testing.T) {
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
		name    string
		fields  fields
		args    args
		wantErr bool
		check   func(*ServiceController) error
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
			check: func(c *ServiceController) error {
				if !c.Services[0].(*dummyService).passedInit {
					return errors.New("first service was not initialized")
				}
				if !c.Services[1].(*dummyService).passedInit {
					return errors.New("second service was not initialized")
				}
				if c.PingPeriod != defaultPingPeriod || c.PingTimeout != defaultPingTimeout {
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
				state:       appStateInit,
				PingTimeout: time.Second * 100,
				PingPeriod:  time.Second * 200,
			},
			args:    args{ctx: context.TODO()},
			wantErr: false,
			check: func(c *ServiceController) error {
				if !c.Services[0].(*dummyService).passedInit {
					return errors.New("first service was not initialized")
				}
				if !c.Services[1].(*dummyService).passedInit {
					return errors.New("second service was not initialized")
				}
				if c.PingPeriod != time.Second*200 || c.PingTimeout != time.Second*100 {
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
			s := &ServiceController{
				Services:    tt.fields.Services,
				PingPeriod:  tt.fields.PingPeriod,
				PingTimeout: tt.fields.PingTimeout,
				stop:        tt.fields.stop,
				state:       tt.fields.state,
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

func TestServiceController_Stop(t *testing.T) {
	t.Run("channel closed", func(t *testing.T) {
		s := &ServiceController{
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
		s := &ServiceController{
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

func TestServiceController_Watch(t *testing.T) {
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
		parallel func(*ServiceController)
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
			parallel: func(s *ServiceController) {
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
			parallel: func(s *ServiceController) {
				<-time.After(time.Millisecond * 5000)
				s.Stop()
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ServiceController{
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

func TestServiceController_checkState(t *testing.T) {
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
			s := &ServiceController{
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

func TestServiceController_cycleTestServices(t *testing.T) {
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
				PingPeriod:  time.Millisecond,
				PingTimeout: time.Millisecond * 5,
				stop:        make(chan struct{}),
				state:       appStateRunning,
			},
			args:    args{ctx: context.TODO()},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ServiceController{
				Services:    tt.fields.Services,
				PingPeriod:  tt.fields.PingPeriod,
				PingTimeout: tt.fields.PingTimeout,
				stop:        tt.fields.stop,
				state:       tt.fields.state,
			}
			if err := s.cycleTestServices(tt.args.ctx); (err != nil) != tt.wantErr {
				t.Errorf("cycleTestServices() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
