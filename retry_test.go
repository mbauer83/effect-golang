package effect_test

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	effect "github.com/mbauer83/effect-golang"
	"github.com/mbauer83/effect-golang/effecttest"
)

func TestRetryNCountsRetriesAndPreservesLastFailure(t *testing.T) {
	var attempts atomic.Int32
	operation := effect.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		attempt := attempts.Add(1)
		return effect.ExitFailure[string, int]("failure-" + strconv.Itoa(int(attempt)))
	})

	exit := effect.Run(context.Background(), effect.Unit{}, operation.RetryN(3))
	if attempts.Load() != 4 {
		t.Fatalf("expected initial attempt and three retries, got %d attempts", attempts.Load())
	}
	cause, ok := exit.Cause()
	if !ok {
		t.Fatal("expected exhausted retry to fail")
	}
	failure, ok := cause.Failure()
	if !ok || failure != "failure-4" {
		t.Fatalf("expected last typed failure, got %+v", cause)
	}
}

func TestRetryStopsAfterSuccess(t *testing.T) {
	var attempts atomic.Int32
	operation := effect.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		if attempts.Add(1) < 3 {
			return effect.ExitFailure[string, int]("transient")
		}
		return effect.ExitSuccess[string](42)
	})

	exit := effect.Run(context.Background(), effect.Unit{}, operation.Retry(effect.Forever[string]()))
	value, ok := exit.Value()
	if !ok || value != 42 || attempts.Load() != 3 {
		t.Fatalf("unexpected retry result: exit=%+v attempts=%d", exit, attempts.Load())
	}
}

func TestRetryNeverRetriesDefects(t *testing.T) {
	var attempts atomic.Int32
	operation := effect.From(func(context.Context, effect.Unit) effect.Exit[string, effect.Unit] {
		attempts.Add(1)
		panic("broken invariant")
	})

	exit := effect.Run(context.Background(), effect.Unit{}, operation.RetryN(10))
	cause, ok := exit.Cause()
	if !ok || !cause.ContainsDefect() || attempts.Load() != 1 {
		t.Fatalf("defect was retried or lost: cause=%+v attempts=%d", cause, attempts.Load())
	}
}

func TestCompositeTypedFailureRequiresRetryCause(t *testing.T) {
	composite := effect.FailCause("left").Both(effect.FailCause("right"))
	var exactAttempts atomic.Int32
	exact := effect.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		exactAttempts.Add(1)
		return effect.ExitCause[string, int](composite)
	})

	exactExit := effect.Run(context.Background(), effect.Unit{}, exact.RetryN(1))
	if !exactExit.IsFailure() || exactAttempts.Load() != 1 {
		t.Fatalf("ordinary Retry must reject a composite cause, attempts=%d", exactAttempts.Load())
	}

	var causeAttempts atomic.Int32
	causeAware := effect.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		if causeAttempts.Add(1) == 1 {
			return effect.ExitCause[string, int](composite)
		}
		return effect.ExitSuccess[string](7)
	})
	causeExit := effect.Run(
		context.Background(),
		effect.Unit{},
		causeAware.RetryCause(effect.Recurs[effect.Cause[string]](1)),
	)
	value, ok := causeExit.Value()
	if !ok || value != 7 || causeAttempts.Load() != 2 {
		t.Fatalf("RetryCause did not retry all-failure cause: exit=%+v attempts=%d", causeExit, causeAttempts.Load())
	}
}

func TestRetryUsesDeterministicCappedBackoff(t *testing.T) {
	clock := effecttest.NewManualClock(time.Unix(0, 0))
	runtime, err := effect.NewRuntime(effect.WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}

	var attempts atomic.Int32
	operation := effect.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		attempt := attempts.Add(1)
		if attempt <= 3 {
			return effect.ExitFailure[string, int]("transient")
		}
		return effect.ExitSuccess[string](99)
	})
	policy := effect.AndSchedules(
		effect.Recurs[string](3),
		effect.Exponential[string](time.Second, 2*time.Second),
	)
	result := make(chan effect.Exit[string, int], 1)
	go func() {
		result <- runtime.Run(context.Background(), effect.Unit{}, operation.Retry(policy))
	}()

	clock.AwaitSleepers(t, 1)
	clock.Advance(time.Second)
	clock.AwaitSleepers(t, 1)
	clock.Advance(2 * time.Second)
	clock.AwaitSleepers(t, 1)
	clock.Advance(2 * time.Second)

	exit := <-result
	value, ok := exit.Value()
	if !ok || value != 99 || attempts.Load() != 4 {
		t.Fatalf("unexpected backoff result: exit=%+v attempts=%d", exit, attempts.Load())
	}
	if got := clock.Now(); !got.Equal(time.Unix(5, 0)) {
		t.Fatalf("expected capped delays totaling five seconds, got %v", got)
	}
}

func TestScheduleHasFreshDriverForEachConcurrentRun(t *testing.T) {
	policy := effect.Recurs[string](1)
	results := make(chan effect.Exit[string, int], 2)

	for range 2 {
		var attempts atomic.Int32
		operation := effect.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
			if attempts.Add(1) == 1 {
				return effect.ExitFailure[string, int]("once")
			}
			return effect.ExitSuccess[string](int(attempts.Load()))
		})
		go func() {
			results <- effect.Run(context.Background(), effect.Unit{}, operation.Retry(policy))
		}()
	}

	for range 2 {
		exit := <-results
		value, ok := exit.Value()
		if !ok || value != 2 {
			t.Fatalf("schedule driver state leaked between runs: %+v", exit)
		}
	}
}

func TestRepeatReturnsFinalScheduleOutput(t *testing.T) {
	var runs atomic.Int32
	operation := effect.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		return effect.ExitSuccess[string](int(runs.Add(1)))
	})

	exit := effect.Run(context.Background(), effect.Unit{}, operation.Repeat(effect.Recurs[int](2)))
	output, ok := exit.Value()
	if !ok || output != 2 || runs.Load() != 3 {
		t.Fatalf("unexpected repeat result: exit=%+v runs=%d", exit, runs.Load())
	}
}

func TestRetryOrElseReceivesLastFailureAndScheduleOutput(t *testing.T) {
	var attempts atomic.Int32
	operation := effect.From(func(context.Context, effect.Unit) effect.Exit[string, string] {
		attempt := attempts.Add(1)
		return effect.ExitFailure[string, string]("failure-" + strconv.Itoa(int(attempt)))
	})
	program := operation.RetryOrElse(
		effect.Recurs[string](2),
		func(last string, output uint64) effect.Effect[effect.Unit, string, string] {
			return effect.Succeed[effect.Unit, string](last + "/retries-" + strconv.FormatUint(output, 10))
		},
	)

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	value, ok := exit.Value()
	if !ok || value != "failure-3/retries-2" || attempts.Load() != 3 {
		t.Fatalf("unexpected retry fallback result: exit=%+v attempts=%d", exit, attempts.Load())
	}
}
