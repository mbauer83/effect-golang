package effect_test

import (
	"context"
	"errors"
	"testing"
	"time"

	effect "github.com/mbauer83/effect-golang"
	"github.com/mbauer83/effect-golang/effecttest"
)

func TestTimeoutReportsWhetherTheWorkCompleted(t *testing.T) {
	runtime, clock := effecttest.NewTimedRuntime(t)
	tracker := &effecttest.Tracker{}
	work := newBlocker(tracker)

	result := make(chan effect.Exit[string, effect.Either[effect.Unit, string]], 1)
	go func() {
		result <- runtime.Run(context.Background(), effect.Unit{},
			effect.Timeout(work.program(), 5*time.Second))
	}()

	work.awaitStart()
	clock.AwaitSleepers(t, 1)
	clock.Advance(5 * time.Second)

	exit := <-result
	value, ok := exit.Value()
	if !ok || !value.IsLeft() {
		t.Fatalf("expected a timed-out result, got %v", exit)
	}
	if got := tracker.Count("interrupted"); got != 1 {
		t.Fatalf("expected the abandoned work to observe interruption, got %d", got)
	}
}

func TestTimeoutYieldsTheValueWhenWorkCompletesFirst(t *testing.T) {
	runtime, _ := effecttest.NewTimedRuntime(t)
	operations := effect.For[effect.Unit, string]()

	exit := runtime.Run(context.Background(), effect.Unit{},
		effect.Timeout(operations.Succeed("prompt"), 5*time.Second))

	value, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected failure: %v", exit)
	}
	completed, ok := value.RightValue()
	if !ok || completed != "prompt" {
		t.Fatalf("expected the completed value, got %v", value)
	}
}

func TestTimeoutFailUsesTheCallersErrorVocabulary(t *testing.T) {
	runtime, clock := effecttest.NewTimedRuntime(t)
	tracker := &effecttest.Tracker{}
	work := newBlocker(tracker)

	result := make(chan effect.Exit[string, string], 1)
	go func() {
		result <- runtime.Run(context.Background(), effect.Unit{},
			work.program().TimeoutFail(2*time.Second, "import timed out"))
	}()

	work.awaitStart()
	clock.AwaitSleepers(t, 1)
	clock.Advance(2 * time.Second)

	exit := <-result
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected the caller's timeout failure, got %v", exit)
	}
	if failure, ok := cause.Failure(); !ok || failure != "import timed out" {
		t.Fatalf("expected the typed timeout failure, got %v", cause)
	}
}

func TestTimeoutToSubstitutesAFallback(t *testing.T) {
	runtime, clock := effecttest.NewTimedRuntime(t)
	tracker := &effecttest.Tracker{}
	work := newBlocker(tracker)

	result := make(chan effect.Exit[string, string], 1)
	go func() {
		result <- runtime.Run(context.Background(), effect.Unit{},
			work.program().TimeoutTo(time.Second, "cached"))
	}()

	work.awaitStart()
	clock.AwaitSleepers(t, 1)
	clock.Advance(time.Second)

	exit := <-result
	if value, ok := exit.Value(); !ok || value != "cached" {
		t.Fatalf("expected the fallback value, got %v", exit)
	}
}

func TestTimeoutAwaitsTheAbandonedWorkFinalizers(t *testing.T) {
	runtime, clock := effecttest.NewTimedRuntime(t)
	tracker := &effecttest.Tracker{}
	work := newBlocker(tracker)

	guarded := effect.Scoped(func(scope effect.Scope) forkedProgram {
		return acquire(tracker, scope, "import-handle").AndThen(work.program())
	})

	result := make(chan effect.Exit[string, string], 1)
	go func() {
		result <- runtime.Run(context.Background(), effect.Unit{},
			guarded.TimeoutFail(3*time.Second, "timed out"))
	}()

	work.awaitStart()
	clock.AwaitSleepers(t, 1)
	clock.Advance(3 * time.Second)

	exit := <-result
	if exit.IsSuccess() {
		t.Fatalf("expected the timeout to win, got %v", exit)
	}
	if got := tracker.Count("release import-handle"); got != 1 {
		t.Fatalf("expected cleanup to complete before Timeout returned, got %d in %v", got, tracker.Events())
	}
}

func TestTimeoutPreservesACleanupDefectFromAbandonedWork(t *testing.T) {
	runtime, clock := effecttest.NewTimedRuntime(t)
	tracker := &effecttest.Tracker{}
	work := newBlocker(tracker)
	broken := errors.New("rollback failed")

	guarded := work.program().Ensuring(effect.Release[effect.Unit](func(context.Context) error {
		return broken
	}))

	result := make(chan effect.Exit[string, string], 1)
	go func() {
		result <- runtime.Run(context.Background(), effect.Unit{},
			guarded.TimeoutFail(time.Second, "timed out"))
	}()

	work.awaitStart()
	clock.AwaitSleepers(t, 1)
	clock.Advance(time.Second)

	exit := <-result
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected a failure, got %v", exit)
	}
	if failures := cause.Failures(); len(failures) != 1 || failures[0] != "timed out" {
		t.Fatalf("expected the timeout failure to survive, got %#v", failures)
	}
	if !cause.ContainsDefect() {
		t.Fatalf("expected the abandoned work's cleanup defect to survive, got %v", cause)
	}
}
