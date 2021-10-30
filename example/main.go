package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/iv-menshenin/appctl"
)

func logError(err error) {
	if err = fmt.Errorf("%w", err); err != nil {
		println(err.Error())
	}
}

// https://opendata.trudvsem.ru/api/v1/vacancies?text=%D0%BF%D1%80%D0%BE%D0%B3%D1%80%D0%B0%D0%BC%D0%BC%D0%B8%D1%81%D1%82&offset=0&limit=1&modifiedFrom=2021-10-24T00:00:00Z

type server struct {
	server http.Server
}

func (s *server) appStart(ctx context.Context, holdOn <-chan struct{}) error {
	var wg sync.WaitGroup
	s.server = http.Server{
		Addr: ":8900",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wg.Add(1)
			defer wg.Done()
			select {
			case <-holdOn:
				w.WriteHeader(http.StatusServiceUnavailable)
			default:
				<-time.After(time.Second * 7)
				w.Header().Add("Content-Type", "text/plain")
				w.WriteHeader(http.StatusOK)
				if _, err := w.Write([]byte("hello world")); err != nil {
					logError(err)
				}
			}
		}),
		ReadTimeout:       time.Millisecond * 250,
		ReadHeaderTimeout: time.Millisecond * 200,
		WriteTimeout:      time.Second * 30,
		IdleTimeout:       time.Minute * 30,
		MaxHeaderBytes:    1024 * 512,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			return ctx
		},
	}
	go func() {
		<-holdOn
		wg.Wait()
		if err := s.server.Close(); err != nil {
			logError(err)
		}
	}()
	if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func main() {
	var srv server
	var svc = appctl.ServiceController{
		Services: []appctl.Service{},
	}
	var app = appctl.Application{
		MainFunc:           srv.appStart,
		InitFunc:           svc.Init,
		TerminationTimeout: time.Minute,
	}
	if err := app.Run(); err != nil {
		logError(err)
		os.Exit(1)
	}
}
