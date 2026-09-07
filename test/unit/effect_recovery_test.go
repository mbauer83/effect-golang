package unit

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	effect "github.com/mbauer83/effect-golang"
)

func TestCatchAllDoesNotDiscardCompositeCause(t *testing.T) {
	composite := effect.FailCause("first").Then(effect.FailCause("second"))
	operation := effect.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		return effect.ExitCause[string, int](composite)
	})
	var handled atomic.Bool
	recovered := operation.CatchAll(func(string) effect.Effect[effect.Unit, string, int] {
		handled.Store(true)
		return effect.Succeed[effect.Unit, string](1)
	})

	exit := effect.Run(context.Background(), effect.Unit{}, recovered)
	cause, ok := exit.Cause()
	if !ok || handled.Load() || cause.Kind() != effect.CauseThen {
		t.Fatalf("composite cause was handled or changed: %+v", exit)
	}
}

func TestCatchAllMergeTagsHandlerFailure(t *testing.T) {
	operation := effect.Fail[effect.Unit, int]("missing")
	recovered := effect.CatchAllMerge(operation, func(string) effect.Effect[effect.Unit, int, int] {
		return effect.Fail[effect.Unit, int](404)
	})

	exit := effect.Run(context.Background(), effect.Unit{}, recovered)
	cause, ok := exit.Cause()
	failure, exact := cause.Failure()
	if !ok || !exact {
		t.Fatalf("expected exact handler failure: %+v", exit)
	}
	right, tagged := failure.RightValue()
	if !tagged || right != 404 {
		t.Fatalf("handler failure was not tagged Right: %+v", failure)
	}
}

func TestCatchCauseCanRecoverDefectExplicitly(t *testing.T) {
	operation := effect.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		panic("boom")
	})
	recovered := operation.CatchCause(func(cause effect.Cause[string]) effect.Effect[effect.Unit, int, int] {
		if !cause.ContainsDefect() {
			return effect.Fail[effect.Unit, int](1)
		}
		return effect.Succeed[effect.Unit, int](9)
	})

	exit := effect.Run(context.Background(), effect.Unit{}, recovered)
	value, ok := exit.Value()
	if !ok || value != 9 {
		t.Fatalf("unexpected cause recovery: %+v", exit)
	}
}

func TestExitOfReifiesEveryOutcomeIncludingInterruption(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	stop := errors.New("caller stopped")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(stop)

	// ExitOf observes rather than recovers, which is why an interruption stays
	// visible instead of re-interrupting the observer.
	exit := effect.Run(ctx, effect.Unit{}, effect.ExitOf(operations.Succeed("unreachable")))
	observed, ok := exit.Value()
	if !ok {
		t.Fatalf("expected ExitOf to succeed with the observed outcome, got %v", exit)
	}
	cause, failed := observed.Cause()
	if !failed {
		t.Fatalf("expected the observed outcome to be an interruption, got %v", observed)
	}
	if interruption, present := cause.Interruption(); !present || !errors.Is(interruption.Cause, stop) {
		t.Fatalf("expected the cancellation cause, got %v", cause)
	}
}

func TestFoldEliminatesBothChannelsAndKeepsHandlerPanicsAsDefects(t *testing.T) {
	operations := effect.For[effect.Unit, string]()

	summary := effect.Fold(
		operations.Fail[int]("rejected"),
		func(cause effect.Cause[string]) string { return "failed: " + cause.String() },
		func(value int) string { return "succeeded" },
	)
	exit := effect.Run(context.Background(), effect.Unit{}, summary)
	if value, ok := exit.Value(); !ok || value != "failed: Fail(rejected)" {
		t.Fatalf("unexpected exit: %v", exit)
	}

	broken := effect.Fold(
		operations.Succeed(1),
		func(effect.Cause[string]) string { return "unreachable" },
		func(int) string { panic("handler exploded") },
	)
	brokenExit := effect.Run(context.Background(), effect.Unit{}, broken)
	brokenCause, failed := brokenExit.Cause()
	if !failed || !brokenCause.ContainsDefect() {
		t.Fatalf("expected a handler panic to become a defect, got %v", brokenExit)
	}
}

func TestFailWithCausePropagatesACompositeCauseUnchanged(t *testing.T) {
	composite := effect.FailCause("left").Both(
		effect.DieCause[string](effect.Defect{Value: "right"}),
	)

	exit := effect.Run(context.Background(), effect.Unit{},
		effect.FailWithCause[effect.Unit, int](composite))

	cause, failed := exit.Cause()
	if !failed || cause.Kind() != effect.CauseBoth {
		t.Fatalf("expected the composite preserved, got %v", exit)
	}
	if len(cause.Failures()) != 1 || len(cause.Defects()) != 1 {
		t.Fatalf("expected both leaves preserved, got %v", cause)
	}
}

func TestWidenErrorRetypesAnInfallibleEffect(t *testing.T) {
	infallible := effect.Succeed[effect.Unit, effect.Never]("done")
	widened := effect.WidenError[string](infallible)

	// The widened effect composes with failing work, which is the point.
	program := widened.FlatMap(func(value string) effect.Effect[effect.Unit, string, string] {
		return effect.Fail[effect.Unit, string](value + ": rejected")
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected the composed failure, got %v", exit)
	}
	if failure, ok := cause.Failure(); !ok || failure != "done: rejected" {
		t.Fatalf("unexpected cause: %v", cause)
	}
}

func TestFailuresAsDefectsPreservesCauseStructure(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	program := effect.FailuresAsDefects(
		operations.Fail[int]("rejected").Ensuring(
			effect.Release[effect.Unit](func(context.Context) error {
				return errors.New("cleanup failed")
			}),
		),
	)

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed || cause.Kind() != effect.CauseThen {
		t.Fatalf("expected the Then structure preserved, got %v", exit)
	}
	if defects := cause.Defects(); len(defects) != 2 {
		t.Fatalf("expected the rewritten failure and the cleanup defect, got %#v", defects)
	}
	if failures := cause.Failures(); len(failures) != 0 {
		t.Fatalf("expected no typed failures to remain, got %#v", failures)
	}
}
