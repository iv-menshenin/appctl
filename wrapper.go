package appctl

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"
)

type (
	ServiceWrapper struct {
		service Service
		options ServiceOptions

		initialized    bool
		firstTimeError time.Time
		errorRepeats   int64
	}
	ServiceOptions struct {
		RestoringThreshold time.Duration
		MaxErrorRepeats    int64

		PostInitialization      bool
		InitializationThreshold time.Duration

		Logger io.Writer
	}
)

func WrapService(service Service, options ServiceOptions) *ServiceWrapper {
	if options.Logger == nil {
		options.Logger = log.New(os.Stderr, "ServiceWrapper:", log.LstdFlags).Writer()
	}
	return &ServiceWrapper{
		service: service,
		options: options,
	}
}

func (s *ServiceWrapper) Init(ctx context.Context) error {
	err := s.service.Init(ctx)
	if err == nil {
		s.initialized = true
		s.firstTimeError = time.Time{}
		return nil
	}
	s.options.Logger.Write([]byte(fmt.Sprintf("[%s] service initialization: %s\n", s.Ident(), err)))
	if !s.options.PostInitialization {
		return err
	}
	if s.firstTimeError.IsZero() {
		s.firstTimeError = time.Now()
	}
	if time.Since(s.firstTimeError) < s.options.InitializationThreshold {
		s.options.Logger.Write([]byte(fmt.Sprintf("[%s] error skipped\n", s.Ident())))
		return nil
	}
	return err
}

func (s *ServiceWrapper) Ping(ctx context.Context) error {
	if !s.initialized {
		return s.Init(ctx)
	}
	err := s.service.Ping(ctx)
	if err != nil {
		s.errorRepeats++
		if s.firstTimeError.IsZero() {
			s.firstTimeError = time.Now()
		}
		s.options.Logger.Write([]byte(fmt.Sprintf("[%s] service ping: %s\n", s.Ident(), err)))
		if time.Since(s.firstTimeError) < s.options.RestoringThreshold && s.errorRepeats < s.options.MaxErrorRepeats {
			s.options.Logger.Write([]byte(fmt.Sprintf("[%s] error skipped\n", s.Ident())))
			return nil
		}
		return err
	}
	s.firstTimeError = time.Time{}
	s.errorRepeats = 0
	return nil
}

func (s *ServiceWrapper) Close() error {
	return s.service.Close()
}

func (s *ServiceWrapper) Ident() string {
	return s.service.Ident()
}
