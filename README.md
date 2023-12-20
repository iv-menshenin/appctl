# Application Controller

This is the code of the runtime controller, which is described in the article at [habr](https://habr.com/ru/post/586568/)  
The repository also contains a sample application using this controller.

## What kind of creature is that?

Do you know a Golang construction for catching termination process signals like SIGTERM or SIGINT?

```go
func main(){
    sigc := make(chan os.Signal, 1)
    signal.Notify(sigc, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
    go func() {
        // ... do something ...
    }()
    <-sigc	
}
```

You would use it when writing code that needs to receive and process network requests.
However, it can become more complicated than just a `signal.Notify` call due to the external services you use.

What if one of your external services breaks down? Should you stop if it can't operate and can't restore its connection?
You must decide how to handle an abnormal termination signal and how to stop processes that are running in the background.
You need to deal with a lot of complexities every time you try to write service code that will manage the execution time 
of your program. Ensuring that the necessary services are not stopped while the requests being processed at the moment 
are not completed. This is called "graceful shutdown". In addition, it is necessary to constantly check the operability
of these services, for example, periodically polling them with the help of `Ping`.

I wrote this package as supporting material for my article.
Then, I decided to try using it in a commercial project because this project did not have its own framework.
And it worked. The application's behavior meets the necessary conditions,
it pings the services and gracefully terminates its work upon the operating system's request
or forcibly if one or more services cannot restore their functionality.

At the same time, the code of this package remains fairly simple, performing enough functions and providing enough options for customization.
Let's take a look at how it works.

## Simple example of graceful shutdown

The following example shows how easily you can start your HTTP server without worrying about the proper graceful
shutdown of request processing. This simple example does not contain any options and is intended only to demonstrate
ease of use.

```go
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
```

Let's examine what this example consists of. When you run `Application.Run`, the `Application` performs the function that 
was passed to the `MainFunc` field, and if `MainFunc` completes its work, the application stops completely.
That is the main concept. However, what about graceful shutdown? When you send a signal to your program to stop working,
the `Application` receives this signal and executes the command `Server.Shutdown` (for this particular example),
causing the HTTP server to terminate (by execution `Server.Shutdown`), which in turn will stop the execution of the `MainFunc`.

### Options

Now let's take a look at this few options:

- `Resources` - an interface that allows the application to understand the overall system state.
  If the application's resources fail, the application terminates unexpectedly. This is an optional parameter.
- `InitializationTimeout` - this value allows you to limit the time for the application initialization.
  If your application's resources are not ready within the allocated time, the application will terminate unexpectedly.
  The initialization process will be explained further.
- `TerminationTimeout` - this value allows you to limit the time allocated for a graceful shutdown.
  By default, this parameter has a value of one second, which is too short, so it's worth specifying it yourself.

In fact, the `TerminationTimeout` parameter limits the time between receiving a signal from the operating system
and the final exit from the MainFunc function.

### Lifecycle

So, let's figure out how this works. Let's understand what happens after you launch the `Application.Run` method.

First of all, resource initialization occurs, i.e. the `Application.Resources.Init` method is simply launched.
A certain amount of time is allocated for this process, and if the initialization is not successfully completed within
the time specified by the `InitializationTimeout` option, the `Application.Run` method will return an error.
To be precise, the initialization process is not limited in itself - in fact, the time is controlled by the 
`Application.Resources.Init` method, which receives the `context.Context` with a set deadline as an argument.

Of course, all of the above only happens if you have filled in the `Resources` field. Otherwise, initialization is
immediately considered complete without any additional actions. And the lifecycle moves on to the next stage - execution.

The next stage of the lifecycle may last indefinitely. Its duration is equal to the duration of running your own code.
At this stage, the `MainFunc` function is launched and under favorable conditions, the execution of this function
continues until it returns some result.
If the Resources field was set and initialization was performed at the previous stage,
the state of resources is controlled during the execution of the current stage.
What this actually means is that a single execution of `Application.Resources.Watch` is started in parallel with
the execution of `MainFunc`, and if this method terminates, the application will be terminated.
The third condition for terminating the program is the receipt of one of the following signals from 
the operating system: SIGHUP, SIGINT, SIGTERM, or SIGQUIT.

Digging deeper, it becomes clear that the three termination paths I mentioned are not identical,
but all of them are elements of a sequential chain of events.
If you lay these things out sequentially, you get a path like this:
- the signal from the operating system executes the `Application.Shutdown` method, which effectively signals
  the closure of the `halt` channel passed as an attribute to the `MainFunc` function;
- terminating the `MainFunc` function causes the application to attempt to stop resources watching
  by calling `Application.Resources.Stop`, which should implicitly stop the execution of `Application.Resources.Watch`;
- then, `Application.Resources.Release` is called, which should actually release the resources used by the application:
  close connections, flush buffers, and so on.

As you can see, these three elements correspond to three conditions under which the program terminates.
The first item is executed when the program receives a signal from the operating system.
In this case, all three items are executed in sequence. If the application stop is triggered by the termination
of the `MainFunc` function, the process starts directly from the second item and then performs the third.
However, if the stop is caused by an error in the resources, while exiting the `Application.Resources.Watch` function,
the application stop process only executes the third item.

Actually, there are a couple more ways to terminate your application. For example, by directly calling
`Application.Shutdown`, you will trigger the application to follow the same shutdown path as if it were a signal
from the operating system, resulting in a graceful shutdown. However, if you call `Application.Close`,
you will achieve an immediate termination of the application without waiting for `MainFunc` to return
but releasing the resources.

## Resources

Now let's talk a bit about the resources of the application.
The example you can find in the file `example/options/main.go` shows how to implement the `Resources` interface.

The main thing is to observe a few conditions that become obvious when you consider the example more carefully:
- when the `Init` method is executed, it is necessary to provide the behavior of interrupting the process
  by tracking the signal `<-ctx.Done()`. Similar behavior for the `Watch` method, but if this is not done,
  you won't lose much, because an emergency exit from `Watch` is only possible when exiting `Watch`. Pun intended;
- what's much more important is the interdependence between the execution of the `Stop` method and the exit from
  the `Watch` method. The former must ensure the latter. That is, the `Stop` method provides a signal to terminate
  the execution of the `Watch` method, and that's its only function;
- `Release` method is executed exactly before `Run` returns its value.
  Here, you must not forget to flush the buffer, which is very important, otherwise, you may lose some data.
  It's also very important that the execution of this function does not take too long.
  Because this process is not limited in any way, your application may hang for an indefinitely long time,
  waiting for the `Release` method to return.
  It may seem that the `TerminationTimeout` parameter is created precisely to limit this time, however,
  it is intended for other purposes.

I am sure that it is necessary to explain the absence of a time limit on the `Release` method.
I have considered several times adding a separate parameter to this process, and once almost made a serious mistake
trying to fit the call to this method under the `TerminationTimeout` limit along with the process of completing
the `MainFunc`. But in the end, I think you will have no trouble limiting the time allotted for resource release
on your own. After all, it is much worse if data is lost due to poorly configured timeouts,
than if the timeouts for the buffer flush function are not configured. It shouldn't take much time, should it?

## Context

You may have noticed that `Application` implements the `context.Context` interface, and you may find this useful.
This context has no deadline, but it has just one feature: it becomes done (or canceled, if that makes more sense)
in the interval between exiting `MainFunc` and entering `Resources.Release`.

So on the one hand, you can use it to stop some auxiliary background processes, like collecting metrics.
And on the other hand, avoid storing a pointer to the context inside your `Resources` object to use it
in the `Release` method. Keep in mind that when `Release` is executed, the `Context` is no longer actual.

Another detail that you may find annoying, however it makes sense. The `Watch` method is executed in a separate routine,
so its execution is in no way synchronized with the state of the context. Therefore, at the moment when the context
is canceled, we cannot be sure that the `Watch` method has already finished its work. In addition,
stopping the `Watch` method does not block the process of stopping the whole application, as it is not necessary
for maintaining consistency.

What should you do if for some reason you need to make sure that the `Watch` function has completed its work completely?
In this case, you just need to synchronize the return from the `Stop` method with the return from the `Watch` method.
Take a look at the implementation of the `Stop` method in the example, you can implement this with a channel close signal.

```go
func (r *resources) Stop() {
	close(r.close)
	// If you want to synchronize `Stop` and `Watch`.
	<-r.done
}

func (r *resources) Watch(ctx context.Context) error {
  defer close(r.done)
  // then some to do...
```

# Service Keeper

Of course, you won't believe me if I tell you that you will have to organize the watch services on your own.
And you'd be right.

The following shows how you can set up periodic service health polling using `ServiceKeeper`.
You just need to wrap the service in the `Service` interface, which supports the following methods:
- `Init` performs service initialization, such as establishing network connections.
- `Ping` performs a service health check.
- `Close` performs the release of resources.
- `Ident` returns a textual identification of the service for the logging system, for example, to make it clear
  to you which service caused the application to crash.

```go
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

type trudVsem struct {
	mux         sync.RWMutex
	requestTime time.Time
	client      *http.Client
	vacancies   []VacancyRender
}

func (t *trudVsem) Init(context.Context) error {
	t.client = http.DefaultClient
	t.vacancies = make([]VacancyRender, 0, prefetchCount)
	return nil
}

func (t *trudVsem) Ping(context.Context) error {
	if time.Since(t.requestTime).Minutes() > 1 {
		t.requestTime = time.Now()
		go t.refresh()
	}
	return nil
}

func (t *trudVsem) Ident() string {
	return "trudvsem.ru"
}

func (t *trudVsem) Close() error {
	return nil
}
```

You need to specify all the services of your application as elements of the `Services` slice of the `ServiceKeeper`
structure, and this in turn takes care of periodic pings and loss-of-service reaction.
And of course in this case we cannot manage without additional options:
- `PingPeriod` allows you to configure the interval for polling for health. Default = 15 second.
- `PingTimeout` limits the time for which the service must respond to `Ping` execution. Default = 5 second.
- `ShutdownTimeout` limits the time in which all services must have time to release resources. Default = minute.
- `SyncStopWatch` synchronizes the `Stop` and `Watch` methods. If this parameter is set to `true`, the resource release
  process is started only after `Watch` finishes its work.

In the case when your application uses multiple external services, you cannot guarantee that all of these services
will be constantly operational. Perhaps you have a specific scenario for this situation; for example, you can simply
block the execution of requests and return a 502 error immediately, hoping that the reverse proxy will redirect
the request to another operational instance of your application.
For these cases, there is a little trick with the following options:
- `DetectedProblem` intercepts errors that have occurred and can silence them in some cases.
- `Recovered` indicates that the problem that occurred earlier has been resolved.
  This means that repeated pings no longer cause errors.

However, the above trick severely limits its use cases because it treats the entire bundle of services as a single entity.
Here's if we could specify a different recovery time threshold for each service... I'll tell you about it a little later.

## Service Keeper details about Init, Ping and Release

In addition to everything I listed above, it is also necessary to explain the mechanics of how this object works.
When `Application` calls the `Init` method, the following happens: `ServiceKeeper` sequentially calls the `Init`
method of each item in the `Services` list. This allows you to maintain dependencies during initialization.
In other words, if you want specific services to initialize earlier than others - just place them higher
in the `Services` list. So keep in mind when setting the `InitializationTimeout` parameter that all services
will be initialized one after another, not at the same time.

Supporting the same idea of keeping dependencies between services, the `Release` method is executed in reverse order.
That is, the `Close` methods of services are executed one after another starting from the last and ending with
the first one.

The `Ping` method in turn is more economical with respect to time spent, it starts `Ping` all services at the same time.
The process of pinging the services itself is repeated at regular intervals, which is guaranteed by a timer.
This means that there will always be an equal distance between the beginning of two neighboring pings on the timeline.
Therefore, when setting the `PingPeriod` parameter, take into account that by the beginning of the next ping
all services must necessarily have time to respond. The `PingTimeout` parameter which should be set smaller
than `PingPeriod` can help you here, but it will not give an absolute guarantee, because `PingTimeout`
actually affects only the `context.Context` argument of the `Ping` method and cannot imperatively interrupt
the pings currently in progress.

```go
var svc = appctl.ServiceKeeper{
    Services: []appctl.Service{
        serviceDB,    // will be initialized first
        serviceMail,  // depends on the database, so it will be initialized right after it
        servicequeue, // depends on both services, so it is initialized last
    },
    PingTimeout: 10 * time.Second, // there should be enough for everything
    PingPeriod:  30 * time.Second, // must be greater than PingTimeout
}
```

# ServiceWrapper

The `ServiceWrapper` allows you to add a little variety to the life of your application's services.
Consider a situation when your application starts simultaneously with all its dependencies.
That is, database servers, queue servers, and whatever else you want are started at the same time.
It is quite likely that your application will not be able to establish a connection to the required resources
for some time and this is not a particularly critical problem, you just need to wait a little longer.
However, what is true for one service may not be true for another.
That is why you need to configure such things for each server separately.

```go
var svc = appctl.ServiceKeeper{
    Services: []appctl.Service{
        appctl.WrapService(&srv.trudVsem, appctl.ServiceOptions{
            RestoringThreshold:      time.Minute,
            InitializationThreshold: time.Minute,
            PostInitialization:      true,
        }),
    },
}
```

Let's take a look at an example.
Using the `WrapService` function, I have configured the service to be insensitive to failures that last less than a minute.
Due to the `RestoringThreshold` setting, service pings will not return any error in `ServiceKeeper` for a minute,
if serviceability is restored during this time, it will be as if nothing happened. 
And only if the problem persists for more than a minute, an escalation will occur.

The `InitializationThreshold` setting allows you to do the same thing with service initialization.
I mean, the service can ignore the initialization error for a while. And it works in such a way that it does not delay
the overall initialization, because only the first attempt to call `Init` is done synchronously.
Other attempts will be performed when `Ping` is actually called. This works on its own when the `PostInitialization`
parameter is enabled.

So, here is the complete list of options that can be overlaid on top of the service health monitoring process:
- `RestoringThreshold` sets the time for which the service should return to its operational state.
- `MaxErrorRepeats` limits the number of consecutive Ping errors that occur.
  If it is exceeded, it throws an error to the calling side.
- `PostInitialization` allows the Init call to be deferred to a later time,
  applicable if the application can be started without the service.
- `InitializationThreshold` limits the time for deferred initialization.
- `Logger` is used to output logs.

# What else?

In fact, that's all you need to control the runtime of your app. If a signal comes from the operating system,
your `MainFunc` function will be signaled about it in the manner of a closed halt channel, a similar event will
occur if services report a loss of health. It is very important to understand that `Application` itself does
not try to restore connections and fix something, all reconnection attempts and other magic needed to restore
serviceability should be implemented either in the background processes of services or in the `Ping` method.

I have been using this package for some time now to implement all sorts of applications.
And so far I am satisfied with everything. The main thing is that you should not forget to set your own timeout settings.
