package effect

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
)

// RunUntilStopped is the body of a program's main: it interprets program on
// runtime until the program ends or the process is asked to stop, by SIGINT or
// SIGTERM, and then closes the runtime so its observers deliver what they
// hold. Being asked to stop is not a failure. A program that failed returns
// its cause as the error, and a runtime that did not close cleanly adds that.
//
//	if err := effect.RunUntilStopped(runtime, serve(settings)); err != nil {
//		fmt.Fprintln(os.Stderr, err)
//		os.Exit(1)
//	}
func RunUntilStopped[E any](runtime *Runtime, program Effect[Unit, E, Unit]) error {
	within, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var failure error
	if cause, failed := runtime.Run(within, Unit{}, program).Cause(); failed && !cause.HasInterruptsOnly() {
		failure = errors.New(cause.String())
	}
	if cause := runtime.Close(context.Background()); !cause.IsEmpty() {
		failure = errors.Join(failure, errors.New("the runtime did not close cleanly: "+cause.String()))
	}
	return failure
}
