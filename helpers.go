package appctl

import (
	"context"
	"errors"
	"io"
	"sync"
)

func MainWithCloser(fn func() error, closer io.Closer, ignore ...error) func(ctx context.Context, halt <-chan struct{}) error {
	return MainWithCloseContext(
		func(ctx context.Context) error { return fn() },
		func(context.Context) error { return closer.Close() },
		ignore...,
	)
}

func MainWithClose(fn func() error, close func(context.Context) error, ignore ...error) func(ctx context.Context, halt <-chan struct{}) error {
	return MainWithCloseContext(func(context.Context) error { return fn() }, close, ignore...)
}

func MainWithCloseContext(fn func(context.Context) error, close func(context.Context) error, ignore ...error) func(context.Context, <-chan struct{}) error {
	return func(ctx context.Context, halt <-chan struct{}) error {
		var (
			err error
			onc sync.Once
			wgr sync.WaitGroup
		)
		wgr.Add(2)
		go func() {
			defer wgr.Done()
			<-halt
			if e := close(ctx); e != nil {
				onc.Do(func() {
					err = e
				})
			}
		}()
		go func() {
			defer wgr.Done()
			if e := fn(ctx); e != nil {
				onc.Do(func() {
					err = e
				})
			}
		}()
		wgr.Wait()
		if err == nil {
			return nil
		}
		for _, ierr := range ignore {
			if errors.Is(err, ierr) {
				return nil
			}
		}
		return err
	}
}
