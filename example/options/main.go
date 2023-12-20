package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/iv-menshenin/appctl"
)

var app appctl.Application

func main() {
	var res resources
	var srv = http.Server{Addr: ":8080", Handler: makeHandler(&res)}
	app = appctl.Application{
		MainFunc:              appctl.MainWithClose(srv.ListenAndServe, srv.Shutdown, http.ErrServerClosed),
		Resources:             &res,
		TerminationTimeout:    10 * time.Second,
		InitializationTimeout: 10 * time.Second,
	}
	if err := app.Run(); err != nil {
		log.Printf("ERROR %s\n", err)
		os.Exit(1)
	}
	log.Println("TERMINATED NORMALLY")
}

func makeHandler(res *resources) http.HandlerFunc {
	var brkClosed int64
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Requested path: %q\n", r.URL.Path)
		if r.URL.Path == "/exit" {
			app.Shutdown() // graceful shutdown
			select {
			case <-app.Done():
				// In this case, the context is still valid.
				// So this case will not happen unless you exceed TerminationTimeout
				log.Println("REQUEST TERMINATED BY CTX")

			case <-time.After(5 * time.Second):
				log.Println("REQUEST TERMINATED BY ITSELF")
			}
		}
		if r.URL.Path == "/break" {
			if atomic.CompareAndSwapInt64(&brkClosed, 0, 1) {
				close(res.brk)
			}
		}
		w.WriteHeader(http.StatusOK)
	}
}

type resources struct {
	ctx   context.Context
	close chan struct{}
	done  chan struct{}
	brk   chan struct{}
}

func (r *resources) Init(ctx context.Context) error {
	log.Println("INITIALIZATION")
	r.ctx = ctx
	r.close = make(chan struct{})
	r.done = make(chan struct{})
	r.brk = make(chan struct{})

	select {
	case <-time.After(5 * time.Second):
		log.Println("INITIALIZED")
		return nil

	case <-ctx.Done():
		log.Println("STOPPED")
		return ctx.Err()
	}
}

func (r *resources) Watch(ctx context.Context) error {
	defer func() {
		log.Println("WATCH STOP")
		// Does not block the shutdown.
		<-time.After(10 * time.Second)
		log.Println("WATCH STOPPED")
		close(r.done)
	}()
	for {
		select {
		case <-r.close:
			// You can use this signal.c
			break

		case <-r.brk:
			// When service breaks ping
			return fmt.Errorf("something went wrong")

		case <-ctx.Done():
			// Or you can use this.
			return ctx.Err()

		case <-time.After(5 * time.Second):
			log.Println("STATUS PING")
			continue
		}
		return nil
	}
}

func (r *resources) Stop() {
	// Nothing more should be done than to send a signal
	close(r.close)
	// Uncomment this if you want to synchronize `Stop` and `Watch`.
	// <-r.done
}

func (r *resources) Release() error {
	log.Println("RELEASE")
	time.Sleep(5 * time.Second)
	// Confirms that the context is canceled in this phase
	<-r.ctx.Done()
	log.Println("RELEASED")
	return nil
}
