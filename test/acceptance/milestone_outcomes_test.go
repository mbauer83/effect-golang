package acceptance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effecttest"
)

// The acceptance program's non-retryable outcomes. A defect and an interruption
// must each end the workflow after exactly one attempt, and each must still
// release the resource that attempt acquired.

func TestMilestoneNeverRetriesADefect(t *testing.T) {
	runtime, _ := effecttest.NewTimedRuntime(t)
	tracker := &effecttest.Tracker{}
	operations := effect.For[effect.Unit, appError]()

	program := milestone(tracker, func(resource) milestoneEffect[string] {
		return operations.From(func(context.Context, effect.Unit) effect.Exit[appError, string] {
			panic("corrupted state")
		})
	})

	exit := runtime.Run(context.Background(), effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed || !cause.ContainsDefect() {
		t.Fatalf("expected a defect, got %v", exit)
	}
	if got := tracker.Count("acquire"); got != 1 {
		t.Fatalf("expected exactly one attempt, got %d", got)
	}
	if got := tracker.Count("release handle"); got != 1 {
		t.Fatalf("expected the resource released once, got %d", got)
	}
}

func TestMilestoneNeverRetriesAnInterruptionAndStillReleases(t *testing.T) {
	runtime, _ := effecttest.NewTimedRuntime(t)
	tracker := &effecttest.Tracker{}
	stop := errors.New("operator stopped the run")
	ctx, cancel := context.WithCancelCause(context.Background())

	program := milestone(tracker, func(resource) milestoneEffect[string] {
		cancel(stop)
		return effect.Succeed[effect.Unit, appError]("unreachable")
	})

	exit := runtime.Run(ctx, effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected interruption, got %v", exit)
	}
	if interruption, ok := cause.Interruption(); !ok || !errors.Is(interruption.Cause, stop) {
		t.Fatalf("expected the caller's cancellation cause, got %v", cause)
	}
	if got := tracker.Count("acquire"); got != 1 {
		t.Fatalf("expected exactly one attempt, got %d", got)
	}
	if got := tracker.Count("release handle"); got != 1 {
		t.Fatalf("expected the resource released once, got %d", got)
	}
}

func TestMilestoneBackoffIsInterruptibleUnderTheLiveClock(t *testing.T) {
	tracker := &effecttest.Tracker{}
	operations := effect.For[effect.Unit, appError]()
	stop := errors.New("operator stopped the run")
	ctx, cancel := context.WithCancelCause(context.Background())

	// The live clock is used deliberately here: the point is that a real wait
	// is abandoned promptly, not that a virtual one can be advanced.
	slow := effect.Recurs[appError](3).MapOutput(func(uint64) time.Duration { return time.Hour })
	program := effect.Scoped(func(scope effect.Scope) milestoneEffect[string] {
		return scope.AcquireRelease(
			operations.Succeed(resource{Name: "handle"}),
			func(resource) effect.Effect[effect.Unit, effect.Never, effect.Unit] {
				return effecttest.TrackedRelease[effect.Unit](tracker, "release handle")
			},
		).AndThen(operations.Fail[string](appError{Reason: "transient"}))
	}).Retry(effect.IntersectSchedules(slow, effect.Spaced[appError](time.Hour)))

	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel(stop)
	}()

	started := time.Now()
	exit := effect.Run(ctx, effect.Unit{}, program)
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("expected the hour-long backoff to be abandoned, waited %v", elapsed)
	}
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected interruption, got %v", exit)
	}
	if interruption, ok := cause.Interruption(); !ok || !errors.Is(interruption.Cause, stop) {
		t.Fatalf("expected the caller's cancellation cause, got %v", cause)
	}
	if got := tracker.Count("release handle"); got < 1 {
		t.Fatalf("expected the resource released, got %d", got)
	}
}
