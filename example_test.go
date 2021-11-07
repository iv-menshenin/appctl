package appctl

import (
	"context"
	"fmt"
	"time"
)

func ExampleApplication_Run() {
	var svc = ServiceKeeper{
		Services: []Service{
			// some services here
		},
		ShutdownTimeout: time.Second * 10,
		PingPeriod:      time.Second * 30,
	}
	var app = Application{
		MainFunc: func(ctx context.Context, holdOn <-chan struct{}) error {
			select {
			case <-holdOn:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			default:
				// do something
				return nil
			}
		},
		Resources:          &svc,
		TerminationTimeout: time.Second * 30,
	}
	if err := app.Run(); err != nil {
		fmt.Println(err)
	} else {
		fmt.Println("done")
	}
	// Output: done
}
