package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/iv-menshenin/appctl"
)

func logError(err error) {
	if err = fmt.Errorf("%w", err); err != nil {
		println(err)
	}
}

type server struct {
	trudVsem trudVsem
}

func (s *server) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	vacancy, ok := s.trudVsem.GetRandomVacancy()
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Add("Content-Type", "text/html;charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := vacancy.RenderTo(w); err != nil {
		logError(err)
	}
}

func (s *server) appStart(ctx context.Context, halt <-chan struct{}) error {
	var httpServer = http.Server{
		Addr:              ":8900",
		Handler:           s,
		ReadTimeout:       time.Millisecond * 250,
		ReadHeaderTimeout: time.Millisecond * 200,
		WriteTimeout:      time.Second * 30,
		IdleTimeout:       time.Minute * 30,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}
	var errShutdown = make(chan error, 1)
	go func() {
		defer close(errShutdown)
		select {
		case <-halt:
		case <-ctx.Done():
		}
		if err := httpServer.Shutdown(ctx); err != nil {
			errShutdown <- err
		}
	}()
	if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	err, ok := <-errShutdown
	if ok {
		return err
	}
	return nil
}

func main() {
	var srv server
	var svc = appctl.ServiceKeeper{
		Services: []appctl.Service{
			&srv.trudVsem,
		},
		ShutdownTimeout: time.Second * 10,
		PingPeriod:      time.Millisecond * 500,
	}
	var app = appctl.Application{
		MainFunc:           srv.appStart,
		Resources:          &svc,
		TerminationTimeout: time.Second * 10,
	}
	if err := app.Run(); err != nil {
		logError(err)
		os.Exit(1)
	}
}
