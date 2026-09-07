package unit

import (
	"context"
	"errors"
	"reflect"
	"testing"

	effect "github.com/mbauer83/effect-golang"
	"github.com/mbauer83/effect-golang/effecttest"
)

func TestEnsuringRunsForEveryOutcome(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	outcomes := map[string]scopedProgram{
		"success":     operations.Succeed("done"),
		"failure":     operations.Fail[string]("rejected"),
		"defect":      effecttest.Panicking[effect.Unit, string, string]("body exploded"),
		"interrupted": effecttest.SelfInterrupting[effect.Unit, string, string](),
	}

	for name, body := range outcomes {
		tracker := &effecttest.Tracker{}
		effect.Run(context.Background(), effect.Unit{}, body.Ensuring(effecttest.TrackedRelease[effect.Unit](tracker, "finalized")))
		if got := tracker.Count("finalized"); got != 1 {
			t.Fatalf("%s: expected exactly one finalization, got %d", name, got)
		}
	}
}

func TestEnsuringRunsAfterCallerCancellation(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	effect.Run(ctx, effect.Unit{}, operations.Succeed("unreachable").
		Ensuring(effecttest.TrackedRelease[effect.Unit](tracker, "finalized")))

	if got := tracker.Count("finalized"); got != 1 {
		t.Fatalf("expected cleanup to survive caller cancellation, got %d", got)
	}
}

func TestOnExitObservesTheOutcomeBeingFinalized(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}

	program := operations.Fail[string]("rejected").OnExit(
		func(exit effect.Exit[string, string]) effect.Effect[effect.Unit, effect.Never, effect.Unit] {
			return effecttest.TrackedRelease[effect.Unit](tracker, "rollback "+exit.String())
		},
	)

	effect.Run(context.Background(), effect.Unit{}, program)
	want := []string{"rollback Failure(Fail(rejected))"}
	if got := tracker.Events(); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the observed outcome,\nwant %v\ngot  %v", want, got)
	}
}

func TestEnsuringComposesFinalizerDefectAfterOriginalCause(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	broken := errors.New("rollback failed")
	program := operations.Fail[string]("rejected").Ensuring(
		effect.Release[effect.Unit](func(context.Context) error {
			return broken
		}),
	)

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed || cause.Kind() != effect.CauseThen {
		t.Fatalf("expected Then(original, cleanup), got %v", exit)
	}
	if failures := cause.Failures(); !reflect.DeepEqual(failures, []string{"rejected"}) {
		t.Fatalf("expected the original failure to survive, got %#v", failures)
	}
	if defects := cause.Defects(); len(defects) != 1 {
		t.Fatalf("expected the cleanup defect, got %#v", defects)
	}
}

func TestNestedEnsuringRunsInnermostFirst(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}

	program := operations.Succeed("done").
		Ensuring(effecttest.TrackedRelease[effect.Unit](tracker, "inner")).
		Ensuring(effecttest.TrackedRelease[effect.Unit](tracker, "outer"))

	effect.Run(context.Background(), effect.Unit{}, program)
	if got := tracker.Events(); !reflect.DeepEqual(got, []string{"inner", "outer"}) {
		t.Fatalf("expected inner cleanup first, got %v", got)
	}
}
