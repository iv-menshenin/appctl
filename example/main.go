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

func appStart(ctx context.Context, holdOn <-chan struct{}) {
	var wg sync.WaitGroup
	var httpServer = http.Server{
		Addr: ":8900",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wg.Add(1)
			defer wg.Done()
			select {
			case <-holdOn:
				w.WriteHeader(http.StatusServiceUnavailable)
			default:
				<-time.After(time.Second * 7)
				w.WriteHeader(http.StatusOK)
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
		if err := httpServer.Close(); err != nil {
			logError(err)
		}
	}()
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		logError(err)
	}
}

func main() {
	var app = appctl.Application{
		MainFunc:           appStart,
		TerminationTimeout: time.Minute,
	}
	if err := app.Run(); err != nil {
		logError(err)
		os.Exit(1)
	}
}
