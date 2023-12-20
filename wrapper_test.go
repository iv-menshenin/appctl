package appctl

import (
	"context"
	"testing"
	"time"
)

func TestServiceWrapperThatInitDeferred(t *testing.T) {
	t.Parallel()
	t.Run("doNotErrOnInit", func(t *testing.T) {
		t.Parallel()
		ds := dummyService{
			brokeOnInit: true,
		}
		sw := serviceThatInitDeferred(&ds)

		err := sw.Init(context.Background())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		ds.brokeOnInit = false
		err = sw.Init(context.Background())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !ds.passedInit {
			t.Error("not initialized")
		}
	})
	t.Run("errOnInitAfterThreshold", func(t *testing.T) {
		t.Parallel()
		ds := dummyService{
			brokeOnInit: true,
		}
		sw := serviceThatInitDeferred(&ds)

		err := sw.Init(context.Background())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		sw.firstTimeError = time.Now().Add(0 - sw.options.InitializationThreshold - time.Second)
		err = sw.Init(context.Background())
		if err == nil {
			t.Errorf("expected error")
		}
	})
	t.Run("initOnPing", func(t *testing.T) {
		t.Parallel()
		ds := dummyService{
			brokeOnInit: true,
		}
		sw := serviceThatInitDeferred(&ds)

		err := sw.Init(context.Background())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		ds.brokeOnInit = false
		err = sw.Ping(context.Background())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !ds.passedInit {
			t.Error("not initialized")
		}
	})
	t.Run("noDotErrOnPing", func(t *testing.T) {
		t.Parallel()
		ds := dummyService{
			brokeOnInit: true,
		}
		sw := serviceThatInitDeferred(&ds)

		err := sw.Init(context.Background())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		err = sw.Ping(context.Background())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("errOnPing", func(t *testing.T) {
		t.Parallel()
		ds := dummyService{
			brokeOnInit: true,
		}
		sw := serviceThatInitDeferred(&ds)

		err := sw.Init(context.Background())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		ds.brokeOnInit = false
		ds.brokeOnPing = true
		err = sw.Ping(context.Background())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		err = sw.Ping(context.Background())
		if err == nil {
			t.Error("expected error")
		}
	})
}

func serviceThatInitDeferred(s Service) ServiceWrapper {
	return ServiceWrapper{
		service: s,
		options: ServiceOptions{
			PostInitialization:      true,
			InitializationThreshold: time.Minute,
			Logger:                  nilWriter{},
		},
	}
}

func TestServiceWrapperPing(t *testing.T) {
	t.Parallel()
	t.Run("pingThrough", func(t *testing.T) {
		t.Parallel()
		ds := dummyService{}
		sw := initializedServiceWrapperDummy(&ds)
		if err := sw.Ping(context.Background()); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !ds.passedPing {
			t.Error("ping is not passed")
		}
	})
	t.Run("pingError", func(t *testing.T) {
		t.Parallel()
		ds := dummyService{
			brokeOnPing: true,
		}
		sw := initializedServiceWrapperDummy(&ds)
		if err := sw.Ping(context.Background()); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("doNotErrOnPing", func(t *testing.T) {
		t.Parallel()
		ds := dummyService{
			brokeOnPing: true,
		}
		sw := initializedServiceWrapperDummy(&ds)
		sw.options.RestoringThreshold = time.Minute
		if err := sw.Ping(context.Background()); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("errorRepeats", func(t *testing.T) {
		t.Parallel()
		ds := dummyService{
			brokeOnPing: true,
		}
		sw := initializedServiceWrapperDummy(&ds)
		sw.options.MaxErrorRepeats = 2
		if err := sw.Ping(context.Background()); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if err := sw.Ping(context.Background()); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if err := sw.Ping(context.Background()); err == nil {
			t.Error("expected")
		}
	})
	t.Run("errOnPingAfterThreshold", func(t *testing.T) {
		t.Parallel()
		ds := dummyService{
			brokeOnPing: true,
		}
		sw := initializedServiceWrapperDummy(&ds)
		sw.options.RestoringThreshold = time.Minute
		if err := sw.Ping(context.Background()); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		sw.firstTimeError = time.Now().Add(-time.Minute - time.Second)
		if err := sw.Ping(context.Background()); err == nil {
			t.Error("expected")
		}
	})
	t.Run("errOnPing", func(t *testing.T) {
		t.Parallel()
		ds := dummyService{
			brokeOnPing: true,
		}
		sw := initializedServiceWrapperDummy(&ds)
		if err := sw.Ping(context.Background()); err == nil {
			t.Error("expected")
		}
	})
}

func TestServiceWrapperModel(t *testing.T) {
	t.Parallel()
	t.Run("closeErr", func(t *testing.T) {
		t.Parallel()
		ds := dummyService{
			brokeOnClose: true,
		}
		sw := initializedServiceWrapperDummy(&ds)
		if err := sw.Close(); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("close", func(t *testing.T) {
		t.Parallel()
		ds := dummyService{}
		sw := initializedServiceWrapperDummy(&ds)
		if err := sw.Close(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !ds.passedClose {
			t.Error("close is not passed")
		}
	})
	t.Run("ident", func(t *testing.T) {
		t.Parallel()
		ds := dummyService{}
		sw := initializedServiceWrapperDummy(&ds)
		if id := sw.Ident(); id != "dummy" {
			t.Errorf("expected dummy ident, got: %q", id)
		}
	})
	t.Run("initErr", func(t *testing.T) {
		t.Parallel()
		ds := dummyService{
			brokeOnInit: true,
		}
		sw := initializedServiceWrapperDummy(&ds)
		if err := sw.Init(context.Background()); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("init", func(t *testing.T) {
		t.Parallel()
		ds := dummyService{}
		sw := initializedServiceWrapperDummy(&ds)
		if err := sw.Init(context.Background()); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !ds.passedInit {
			t.Error("init is not passed")
		}
	})
}

func initializedServiceWrapperDummy(s Service) ServiceWrapper {
	return ServiceWrapper{
		service:     s,
		initialized: true,
		options: ServiceOptions{
			Logger: nilWriter{},
		},
	}
}

type nilWriter struct{}

func (nilWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func Test_normalizeOptions(t *testing.T) {
	var srv = WrapService(&dummyService{}, ServiceOptions{})
	if srv.options.Logger == nil {
		t.Error("logger is not initialized")
	}
}
