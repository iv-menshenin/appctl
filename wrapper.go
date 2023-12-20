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
		// RestoringThreshold sets the time for which the service should return to its operational state.
		RestoringThreshold time.Duration
		// MaxErrorRepeats limits the number of consecutive Ping errors that occur. If it is exceeded, it throws an error to the calling side.
		MaxErrorRepeats int64
		// PostInitialization allows the Init call to be deferred to a later time, applicable if the application can be started without the service.
		PostInitialization bool
		// InitializationThreshold limits the time for deferred initialization.
		InitializationThreshold time.Duration
		// Logger is used to output logs.
		Logger io.Writer
	}
)

// WrapService allows you to wrap a service by applying deferred initialization or a threshold of failed ping attempts.
func WrapService(service Service, options ServiceOptions) *ServiceWrapper {
	return &ServiceWrapper{
		service: service,
		options: normalizeOptions(options),
	}
}

func normalizeOptions(options ServiceOptions) ServiceOptions {
	if options.Logger == nil {
		options.Logger = log.New(os.Stderr, "ServiceWrapper:", log.LstdFlags).Writer()
	}
	return options
}

func (s *ServiceWrapper) Init(ctx context.Context) error {
	err := s.service.Init(ctx)
	if err == nil {
		s.initialized = true
		s.firstTimeError = time.Time{}
		return nil
	}
	s.log("service initialization: %s", err)
	if !s.options.PostInitialization {
		return err
	}
	if s.firstTimeError.IsZero() {
		s.firstTimeError = time.Now()
	}
	if time.Since(s.firstTimeError) < s.options.InitializationThreshold {
		s.log("error skipped")
		return nil
	}
	return err
}

func (s *ServiceWrapper) Ping(ctx context.Context) error {
	if !s.initialized {
		return s.Init(ctx)
	}
	err := s.service.Ping(ctx)
	if err == nil {
		s.firstTimeError = time.Time{}
		s.errorRepeats = 0
		return nil
	}
	s.errorRepeats++
	if s.firstTimeError.IsZero() {
		s.firstTimeError = time.Now()
	}
	s.log("service ping: %s", err)
	timeDelayed := s.options.RestoringThreshold > 0 && time.Since(s.firstTimeError) <= s.options.RestoringThreshold
	countDelayed := s.options.MaxErrorRepeats > 0 && s.errorRepeats <= s.options.MaxErrorRepeats
	if timeDelayed || countDelayed {
		s.log("error skipped")
		return nil
	}
	return err
}

func (s *ServiceWrapper) log(format string, a ...any) {
	if s.options.Logger == nil {
		return
	}
	_, _ = s.options.Logger.Write([]byte(fmt.Sprintf("[%s] %s\n", s.Ident(), fmt.Sprintf(format, a...))))
}

func (s *ServiceWrapper) Close() error {
	return s.service.Close()
}

func (s *ServiceWrapper) Ident() string {
	return s.service.Ident()
}
