package main

import (
	"github.com/iv-menshenin/appctl"
	"log"
	"net/http"
	"os"
)

func main() {
	var srv = http.Server{Addr: ":8080", Handler: http.HandlerFunc(handler)}
	var app = appctl.Application{
		MainFunc: appctl.MainWithClose(srv.ListenAndServe, srv.Shutdown, http.ErrServerClosed),
	}
	if err := app.Run(); err != nil {
		log.Printf("ERROR %s\n", err)
		os.Exit(1)
	}
	log.Println("TERMINATED NORMALLY")
}

func handler(w http.ResponseWriter, r *http.Request) {
	log.Printf("Requested path: %q\n", r.URL.Path)
	w.WriteHeader(http.StatusOK)
}
