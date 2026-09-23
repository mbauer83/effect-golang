package unit

// Where a failure came from, carried with it.
//
// The two things a reader asks when a cause reaches them -- which line
// produced this, and what was going on at the time -- and both are learned in
// different places: the line where the failure is written, the span only when
// it is run.

import (
	"context"
	"errors"
	"fmt"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
)

func TestAFailureSaysWhichLineRaisedItAndWhatWasRunning(t *testing.T) {
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	operations := effect.For[effect.Unit, error]()

	failing, where := failHere(errors.New("the account is frozen"))
	program := operations.Succeed(0).
		FlatMap(func(int) effect.Effect[effect.Unit, error, int] { return failing }).
		WithSpan("charging the account")

	cause, failed := runtime.Run(context.Background(), effect.Unit{}, program).Cause()
	if !failed {
		t.Fatal("expected the failure")
	}

	raised := cause.Origin()
	if raised.Source != where {
		t.Errorf("expected the line that raised it (%s), got %q", where, raised.Source)
	}
	if raised.Operation != "charging the account" {
		t.Errorf("expected the span it was raised inside, got %q", raised.Operation)
	}
	// And both in the rendering, on the same line as the failure, because a
	// reader asking what went wrong is about to ask where.
	rendered := cause.String()
	if !strings.Contains(rendered, where) ||
		!strings.Contains(rendered, "charging the account") {
		t.Errorf("expected the rendering to say where, got %q", rendered)
	}
}

func TestTranslatingAFailureKeepsWhereItWasRaised(t *testing.T) {
	// The property that makes this useful across a hexagon. A store's fault
	// becomes the domain's at the boundary, and the line worth opening is
	// where it happened rather than where it was translated -- so a mapped
	// failure keeps its own.
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}

	raising, where := failHere(errors.New("no such row"))
	translated := raising.MapError(func(error) error { return errors.New("nothing kept") })

	cause, failed := runtime.Run(context.Background(), effect.Unit{}, translated).Cause()
	if !failed {
		t.Fatal("expected the failure")
	}
	if cause.Origin().Source != where {
		t.Errorf("expected the line that raised it (%s) rather than the line that mapped it, got %q",
			where, cause.Origin().Source)
	}
}

func TestAFailureRaisedThroughTheHandleNamesTheCallerNotTheHandle(t *testing.T) {
	// A cause that named a line inside the framework would send a reader to
	// read the framework.
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	operations := effect.For[effect.Unit, error]()

	failing, where := failHereVia(operations, errors.New("refused"))

	cause, failed := runtime.Run(context.Background(), effect.Unit{}, failing).Cause()
	if !failed {
		t.Fatal("expected the failure")
	}
	if cause.Origin().Source != where {
		t.Errorf("expected the caller's line (%s), got %q", where, cause.Origin().Source)
	}
}

// failHere is a failure and the line it was raised on.
//
// The line is taken from the frame above the raise rather than written down,
// because a test that hard-codes a number breaks whenever anything above it
// is edited -- which makes it a test of the file's layout rather than of the
// location.
func failHere(failure error) (effect.Effect[effect.Unit, error, int], string) {
	_, file, line, _ := goruntime.Caller(0)
	return effect.Fail[effect.Unit, int](failure), fmt.Sprintf("%s:%d", file, line+1)
}

// failHereVia is the same through an operations handle.
func failHereVia(
	operations effect.Operations[effect.Unit, error],
	failure error,
) (effect.Effect[effect.Unit, error, int], string) {
	_, file, line, _ := goruntime.Caller(0)
	return operations.Fail[int](failure), fmt.Sprintf("%s:%d", file, line+1)
}
