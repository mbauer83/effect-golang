package unit

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effecttest"
)

// The boundary cases the runtime design lists explicitly: a zero retry budget,
// non-positive delays, the two scope placements, the outcomes that stop
// repetition, and the stack behaviour of a very long attempt sequence.

func TestManyRetryAttemptsAddNoStackFrames(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	attempts := 0
	flaky := operations.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		attempts++
		if attempts < deepDepth {
			return effect.ExitFailure[string, int]("transient")
		}
		return effect.ExitSuccess[string](attempts)
	})

	exit := effect.Run(context.Background(), effect.Unit{}, flaky.RetryN(deepDepth))
	value, ok := exit.Value()
	if !ok || value != deepDepth {
		t.Fatalf("unexpected result after %d attempts: %v", attempts, exit)
	}
}

func TestRetryNZeroAllowsOnlyTheInitialAttempt(t *testing.T) {
	var attempts atomic.Int32
	operation := effect.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		attempts.Add(1)
		return effect.ExitFailure[string, int]("transient")
	})

	exit := effect.Run(context.Background(), effect.Unit{}, operation.RetryN(0))
	if got := attempts.Load(); got != 1 {
		t.Fatalf("expected exactly one attempt, got %d", got)
	}
	if cause, failed := exit.Cause(); !failed || cause.Kind() != effect.CauseFailure {
		t.Fatalf("expected the original failure, got %v", exit)
	}
}

func TestNonPositiveDelaysNormalizeToNoWait(t *testing.T) {
	runtime, clock := effecttest.NewTimedRuntime(t)
	var attempts atomic.Int32
	operation := effect.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		if attempts.Add(1) < 3 {
			return effect.ExitFailure[string, int]("transient")
		}
		return effect.ExitSuccess[string](1)
	})

	// A negative spacing must not become a wait the test has to release, and
	// must not move the clock backwards.
	negative := effect.IntersectSchedules(
		effect.Recurs[string](5),
		effect.Spaced[string](-time.Hour),
	)
	exit := runtime.Run(context.Background(), effect.Unit{}, operation.Retry(negative))

	if exit.IsFailure() {
		t.Fatalf("unexpected failure: %v", exit)
	}
	if got := clock.PendingSleeps(); got != 0 {
		t.Fatalf("expected no pending wait, got %d", got)
	}
	if got := clock.Now(); !got.Equal(time.Unix(0, 0)) {
		t.Fatalf("expected the clock not to move, got %v", got)
	}
}

func TestScopeOutsideRetrySharesOneLifetimeAcrossAttempts(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}
	var attempts atomic.Int32

	// Scoped outside Retry: every attempt shares one lifetime, so the resource
	// is acquired once and released once, after the last attempt.
	program := effect.Scoped(func(scope effect.Scope) effect.Effect[effect.Unit, string, string] {
		return effecttest.TrackedResource[effect.Unit, string](scope, tracker, "shared").FlatMap(func(string) effect.Effect[effect.Unit, string, string] {
			return operations.From(func(context.Context, effect.Unit) effect.Exit[string, string] {
				if attempts.Add(1) < 3 {
					return effect.ExitFailure[string, string]("transient")
				}
				return effect.ExitSuccess[string]("recovered")
			}).RetryN(5)
		})
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if value, ok := exit.Value(); !ok || value != "recovered" {
		t.Fatalf("unexpected exit: %v", exit)
	}
	want := []string{"acquire shared", "release shared"}
	if got := tracker.Events(); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected one shared lifetime,\nwant %v\ngot  %v", want, got)
	}
}

func TestScopeInsideRetryGivesEachAttemptItsOwnResource(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}
	var attempts atomic.Int32

	attempt := effect.Scoped(func(scope effect.Scope) effect.Effect[effect.Unit, string, string] {
		return effecttest.TrackedResource[effect.Unit, string](scope, tracker, "per-attempt").AndThen(
			operations.From(func(context.Context, effect.Unit) effect.Exit[string, string] {
				if attempts.Add(1) < 3 {
					return effect.ExitFailure[string, string]("transient")
				}
				return effect.ExitSuccess[string]("recovered")
			}),
		)
	})

	exit := effect.Run(context.Background(), effect.Unit{}, attempt.RetryN(5))
	if exit.IsFailure() {
		t.Fatalf("unexpected failure: %v", exit)
	}
	if acquired := tracker.Count("acquire per-attempt"); acquired != 3 {
		t.Fatalf("expected one acquisition per attempt, got %d", acquired)
	}
	if released := tracker.Count("release per-attempt"); released != 3 {
		t.Fatalf("expected one release per attempt, got %d", released)
	}
}

func TestPanickingFinalizerBecomesADefectComposedAfterTheOriginalCause(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	program := effect.Scoped(func(scope effect.Scope) effect.Effect[effect.Unit, string, string] {
		return scope.AcquireRelease(
			operations.Succeed("handle"),
			func(string) effect.Effect[effect.Unit, effect.Never, effect.Unit] {
				return effect.From(func(context.Context, effect.Unit) effect.Exit[effect.Never, effect.Unit] {
					panic("finalizer exploded")
				})
			},
		).AndThen(operations.Fail[string]("rejected"))
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed || cause.Kind() != effect.CauseThen {
		t.Fatalf("expected Then(original, defect), got %v", exit)
	}
	if failures := cause.Failures(); !reflect.DeepEqual(failures, []string{"rejected"}) {
		t.Fatalf("expected the original failure preserved, got %#v", failures)
	}
	defects := cause.Defects()
	if len(defects) != 1 || defects[0].Value != "finalizer exploded" {
		t.Fatalf("expected the finalizer panic captured, got %#v", defects)
	}
	if defects[0].Stack == "" {
		t.Fatal("expected the finalizer panic to carry a stack")
	}
}

func TestRepeatStopsOnADefectAndOnInterruption(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	policy := effect.Recurs[int](10)

	var panicking atomic.Int32
	defecting := operations.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		if panicking.Add(1) >= 2 {
			panic("corrupted state")
		}
		return effect.ExitSuccess[string](1)
	})
	exit := effect.Run(context.Background(), effect.Unit{}, defecting.Repeat(policy))
	if cause, failed := exit.Cause(); !failed || !cause.ContainsDefect() {
		t.Fatalf("expected repetition to stop on a defect, got %v", exit)
	}
	if got := panicking.Load(); got != 2 {
		t.Fatalf("expected exactly two runs, got %d", got)
	}

	stop := errors.New("caller stopped repeating")
	ctx, cancel := context.WithCancelCause(context.Background())
	var runs atomic.Int32
	cancelling := operations.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		if runs.Add(1) >= 2 {
			cancel(stop)
		}
		return effect.ExitSuccess[string](1)
	})
	interrupted := effect.Run(ctx, effect.Unit{}, cancelling.Repeat(policy))
	cause, failed := interrupted.Cause()
	if !failed || !cause.IsInterruptedOnly() {
		t.Fatalf("expected repetition to stop on interruption, got %v", interrupted)
	}
	if interruption, ok := cause.Interruption(); !ok || !errors.Is(interruption.Cause, stop) {
		t.Fatalf("expected the caller's cancellation cause, got %v", cause)
	}
}
