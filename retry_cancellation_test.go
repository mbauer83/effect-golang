package effect_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	effect "github.com/mbauer83/effect-golang"
	"github.com/mbauer83/effect-golang/effecttest"
)

func TestRetryCancellationDuringAttemptWinsOverTypedFailure(t *testing.T) {
	started := make(chan struct{})
	operation := effect.From(func(ctx context.Context, _ effect.Unit) effect.Exit[string, int] {
		close(started)
		<-ctx.Done()
		return effect.ExitFailure[string, int]("reported after cancellation")
	})
	ctx, cancel := context.WithCancelCause(context.Background())
	stop := errors.New("stop attempt")
	result := make(chan effect.Exit[string, int], 1)
	go func() {
		result <- effect.Run(ctx, effect.Unit{}, operation.Retry(effect.Forever[string]()))
	}()

	<-started
	cancel(stop)
	assertInterruptedBy(t, <-result, stop)
}

func TestRetryCancellationDuringDelayPreservesCause(t *testing.T) {
	clock := effecttest.NewManualClock(time.Unix(0, 0))
	runtime, err := effect.NewRuntime(effect.WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	var attempts atomic.Int32
	operation := effect.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		attempts.Add(1)
		return effect.ExitFailure[string, int]("retryable")
	})
	ctx, cancel := context.WithCancelCause(context.Background())
	stop := errors.New("stop delay")
	result := make(chan effect.Exit[string, int], 1)
	go func() {
		result <- runtime.Run(ctx, effect.Unit{}, operation.Retry(effect.Spaced[string](time.Hour)))
	}()

	clock.AwaitSleepers(t, 1)
	cancel(stop)
	exit := <-result
	assertInterruptedBy(t, exit, stop)
	if attempts.Load() != 1 || clock.PendingSleeps() != 0 {
		t.Fatalf("retry continued after cancellation: attempts=%d pending=%d", attempts.Load(), clock.PendingSleeps())
	}
}

func TestRetryCauseRejectsCompositeContainingDefect(t *testing.T) {
	var attempts atomic.Int32
	cause := effect.FailCause("typed").Then(effect.DieCause[string](effect.Defect{Value: "broken"}))
	operation := effect.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		attempts.Add(1)
		return effect.ExitCause[string, int](cause)
	})

	exit := effect.Run(
		context.Background(),
		effect.Unit{},
		operation.RetryCause(effect.Recurs[effect.Cause[string]](5)),
	)
	resultCause, ok := exit.Cause()
	if !ok || !resultCause.ContainsDefect() || attempts.Load() != 1 {
		t.Fatalf("unsafe composite was retried or changed: cause=%+v attempts=%d", resultCause, attempts.Load())
	}
}

func TestRepeatStopsOnTypedFailure(t *testing.T) {
	var runs atomic.Int32
	operation := effect.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		if runs.Add(1) == 1 {
			return effect.ExitSuccess[string](1)
		}
		return effect.ExitFailure[string, int]("stop repeating")
	})

	exit := effect.Run(context.Background(), effect.Unit{}, operation.Repeat(effect.Forever[int]()))
	cause, ok := exit.Cause()
	failure, exact := cause.Failure()
	if !ok || !exact || failure != "stop repeating" || runs.Load() != 2 {
		t.Fatalf("repeat did not preserve failure: exit=%+v runs=%d", exit, runs.Load())
	}
}

func assertInterruptedBy[E, A any](t *testing.T, exit effect.Exit[E, A], expected error) {
	t.Helper()
	cause, ok := exit.Cause()
	if !ok {
		t.Fatal("expected interruption")
	}
	interruption, ok := cause.Interruption()
	if !ok || !errors.Is(interruption.Cause, expected) {
		t.Fatalf("unexpected interruption: %+v", cause)
	}
}
