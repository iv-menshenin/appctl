package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/iv-menshenin/appctl"
)

func logError(err error) {
	if err = fmt.Errorf("%w", err); err != nil {
		println(err.Error())
	}
}

type server struct {
	trudVsem trudVsem
}

func (s *server) getVacancy() ([]byte, error) {
	vacancy, ok := s.trudVsem.GetRandom()
	if !ok {
		return nil, nil
	}
	var b []byte
	var w = bytes.NewBuffer(b)
	if _, err := w.WriteString(fmt.Sprintf("<h3>%s (%s)</h3>", vacancy.JobName, vacancy.Region.Name)); err != nil {
		return nil, err
	}
	if _, err := w.WriteString(fmt.Sprintf("<p class='description'>Компания: %s ищет сотрудника на должность '%s'.</p>", vacancy.Company.Name, vacancy.JobName)); err != nil {
		return nil, err
	}
	if _, err := w.WriteString(fmt.Sprintf("<p class='condition'>Условия: %s, %s.</p>", vacancy.Employment, vacancy.Schedule)); err != nil {
		return nil, err
	}
	if vacancy.SalaryMin != vacancy.SalaryMax && vacancy.SalaryMax != 0 && vacancy.SalaryMin != 0 {
		if _, err := w.WriteString(fmt.Sprintf("<p class='salary'>зарплата от %0.2f до %0.2f руб.</p>", vacancy.SalaryMin, vacancy.SalaryMax)); err != nil {
			return nil, err
		}
	} else if vacancy.SalaryMax > 0 {
		if _, err := w.WriteString(fmt.Sprintf("<p class='salary'>зарплата %0.2f руб.</p>", vacancy.SalaryMax)); err != nil {
			return nil, err
		}
	} else if vacancy.SalaryMin > 0 {
		if _, err := w.WriteString(fmt.Sprintf("<p class='salary'>зарплата %0.2f руб.</p>", vacancy.SalaryMin)); err != nil {
			return nil, err
		}
	}
	if _, err := w.WriteString(fmt.Sprintf("<a href='%s'>ознакомиться</a>", vacancy.URL)); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

func (s *server) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	data, err := s.getVacancy()
	if err != nil {
		logError(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if len(data) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Add("Content-Type", "text/html;charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err = w.Write(data); err != nil {
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
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
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
