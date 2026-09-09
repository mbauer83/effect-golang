package acceptance

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effecttest"
)

// This file is the runtime design's own acceptance program: a resource-safe
// retry workflow, exercised through every outcome it can have.
//
//	attempt := Scoped(func(scope Scope) Effect[Env, AppError, Result] {
//	    return scope.
//	        AcquireRelease(acquireResource, releaseResource).
//	        FlatMap(useResource)
//	})
//
//	program := attempt.Retry(AndSchedules(
//	    Recurs[AppError](3),
//	    Exponential[AppError](100*time.Millisecond, 5*time.Second),
//	))

type appError struct {
	Reason string
}

type resource struct {
	Name string
}

type milestoneEffect[A any] = effect.Effect[effect.Unit, appError, A]

func backoffPolicy() effect.Schedule[appError, effect.Product[uint64, time.Duration]] {
	return effect.IntersectSchedules(
		effect.Recurs[appError](3),
		effect.Exponential[appError](100*time.Millisecond, 5*time.Second),
	)
}

// milestone builds the acceptance program from a caller-supplied use step, so
// every outcome exercises exactly the same acquisition and cleanup path.
func milestone(
	tracker *effecttest.Tracker,
	use func(resource) milestoneEffect[string],
) milestoneEffect[string] {
	operations := effect.For[effect.Unit, appError]()
	attempt := effect.Scoped(func(scope effect.Scope) milestoneEffect[string] {
		return scope.AcquireRelease(
			operations.From(func(context.Context, effect.Unit) effect.Exit[appError, resource] {
				tracker.Record("acquire")
				return effect.ExitSuccess[appError](resource{Name: "handle"})
			}),
			func(held resource) effect.Effect[effect.Unit, effect.Never, effect.Unit] {
				return effect.AddFinalizer[effect.Unit](func(context.Context) error {
					tracker.Record("release " + held.Name)
					return nil
				})
			},
		).FlatMap(use)
	})
	return attempt.Retry(backoffPolicy())
}

func TestMilestoneConstructionExecutesNothing(t *testing.T) {
	tracker := &effecttest.Tracker{}
	milestone(tracker, func(resource) milestoneEffect[string] {
		tracker.Record("use")
		return effect.Succeed[effect.Unit, appError]("done")
	})

	if events := tracker.Events(); len(events) != 0 {
		t.Fatalf("expected construction to run nothing, got %v", events)
	}
}

func TestMilestoneFirstAttemptIsImmediateAndReleasesItsResource(t *testing.T) {
	runtime, clock := effecttest.NewTimedRuntime(t)
	tracker := &effecttest.Tracker{}
	operations := effect.For[effect.Unit, appError]()

	program := milestone(tracker, func(held resource) milestoneEffect[string] {
		tracker.Record("use " + held.Name)
		return operations.Succeed("done")
	})

	exit := runtime.Run(context.Background(), effect.Unit{}, program)
	if value, ok := exit.Value(); !ok || value != "done" {
		t.Fatalf("unexpected exit: %v", exit)
	}
	if got := clock.PendingSleeps(); got != 0 {
		t.Fatalf("expected no wait before the first attempt, got %d", got)
	}
	if !clock.Now().Equal(time.Unix(0, 0)) {
		t.Fatalf("expected the first attempt to be immediate, clock is at %v", clock.Now())
	}
	assertTrackedOrder(t, tracker, []string{"acquire", "use handle", "release handle"})
}

func assertTrackedOrder(t *testing.T, tracker *effecttest.Tracker, want []string) {
	t.Helper()
	got := tracker.Events()
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestMilestoneRetriesTypedFailuresWithTestClockBackoff(t *testing.T) {
	runtime, clock := effecttest.NewTimedRuntime(t)
	tracker := &effecttest.Tracker{}
	operations := effect.For[effect.Unit, appError]()
	var attempts atomic.Int32

	program := milestone(tracker, func(resource) milestoneEffect[string] {
		if attempts.Add(1) <= 3 {
			return operations.Fail[string](appError{Reason: "transient"})
		}
		return operations.Succeed("recovered")
	})

	result := make(chan effect.Exit[appError, string], 1)
	go func() {
		result <- runtime.Run(context.Background(), effect.Unit{}, program)
	}()

	// The backoff is the intersection of "at most three retries" and capped
	// doubling, so the delays are 100ms, 200ms and 400ms.
	for _, delay := range []time.Duration{100, 200, 400} {
		clock.AwaitSleepers(t, 1)
		clock.Advance(delay * time.Millisecond)
	}

	exit := <-result
	if value, ok := exit.Value(); !ok || value != "recovered" {
		t.Fatalf("unexpected exit: %v", exit)
	}
	if got := attempts.Load(); got != 4 {
		t.Fatalf("expected four evaluations, got %d", got)
	}
	if got := clock.Now(); !got.Equal(time.Unix(0, 0).Add(700 * time.Millisecond)) {
		t.Fatalf("expected 700ms of backoff, clock is at %v", got)
	}

	// Each attempt owned and released its own resource, because Scoped is
	// inside Retry.
	if acquired, released := tracker.Count("acquire"), tracker.Count("release handle"); acquired != 4 || released != 4 {
		t.Fatalf("expected four acquisitions and four releases, got %d and %d", acquired, released)
	}
}

func TestMilestonePreservesTheLastTypedFailureWhenExhausted(t *testing.T) {
	runtime, clock := effecttest.NewTimedRuntime(t)
	tracker := &effecttest.Tracker{}
	operations := effect.For[effect.Unit, appError]()

	program := milestone(tracker, func(resource) milestoneEffect[string] {
		return operations.Fail[string](appError{Reason: "permanent"})
	})

	result := make(chan effect.Exit[appError, string], 1)
	go func() {
		result <- runtime.Run(context.Background(), effect.Unit{}, program)
	}()
	for range 3 {
		clock.AwaitSleepers(t, 1)
		clock.Advance(time.Second)
	}

	exit := <-result
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected exhaustion to fail, got %v", exit)
	}
	failure, isLeaf := cause.Failure()
	if !isLeaf || failure.Reason != "permanent" {
		t.Fatalf("expected the last typed failure, got %v", cause)
	}
	if got := tracker.Count("release handle"); got != 4 {
		t.Fatalf("expected every attempt to release its resource, got %d", got)
	}
}
