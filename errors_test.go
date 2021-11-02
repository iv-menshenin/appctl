package appctl

import (
	"context"
	"errors"
	"testing"
	"time"
)

func Test_appError_Error(t *testing.T) {
	tests := []struct {
		name string
		e    appError
		want string
	}{
		{
			name: "empty",
			e:    "",
			want: "",
		},
		{
			name: "test 1",
			e:    "I can pay",
			want: "I can pay",
		},
		{
			name: "test 2",
			e:    "i have to go",
			want: "i have to go",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.e.Error(); got != tt.want {
				t.Errorf("Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_arrError_Error(t *testing.T) {
	tests := []struct {
		name string
		e    arrError
		want string
	}{
		{
			name: "zero",
			e:    arrError{},
			want: "something went wrong",
		},
		{
			name: "one error",
			e: arrError{
				appError("something went wrong"),
			},
			want: "the following errors occurred:\nsomething went wrong",
		},
		{
			name: "two errors",
			e: arrError{
				appError("first"),
				appError("second"),
			},
			want: "the following errors occurred:\nfirst\nsecond",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.e.Error(); got != tt.want {
				t.Errorf("Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parallelRun_Do(t *testing.T) {
	t.Run("expected error", func(t *testing.T) {
		t.Parallel()
		var p parallelRun
		var e = errors.New("test parallel run error")
		p.do(context.TODO(), func(ctx context.Context) error {
			return e
		})
		expect := arrError{e}
		if err := p.wait(); err.Error() != expect.Error() {
			t.Errorf("expected error '%v', got '%v'", expect, err)
		}
	})
	t.Run("well done", func(t *testing.T) {
		t.Parallel()
		var p parallelRun
		p.do(context.TODO(), func(ctx context.Context) error {
			return nil
		})
		if err := p.wait(); err != nil {
			t.Errorf("unexpected error '%v'", err)
		}
	})
	t.Run("catch panic", func(t *testing.T) {
		t.Parallel()
		var p parallelRun
		p.do(context.TODO(), func(ctx context.Context) error {
			panic("bang-bang")
		})
		if err := p.wait(); err == nil {
			t.Error("panic lost")
		}
	})
	t.Run("multiple run", func(t *testing.T) {
		t.Parallel()
		var p parallelRun
		var e = errors.New("test parallel run error in multiple")
		p.do(context.TODO(), func(ctx context.Context) error {
			return e
		})
		p.do(context.TODO(), func(ctx context.Context) error {
			return nil
		})
		expect := arrError{e}
		if err := p.wait(); err.Error() != expect.Error() {
			t.Errorf("expected error '%v', got '%v'", expect, err)
		}
	})
}

func Test_parallelRun_Wait(t *testing.T) {
	t.Parallel()
	var p parallelRun
	var e = errors.New("test wait for error")
	p.do(context.TODO(), func(ctx context.Context) error {
		<-time.After(time.Millisecond * 5)
		return e
	})
	expect := arrError{e}
	if err := p.wait(); err.Error() != expect.Error() {
		t.Errorf("expected error '%v', got '%v'", expect, err)
	}
}
