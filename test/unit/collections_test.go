package unit

import (
	"context"
	"reflect"
	"testing"
	"time"

	effect "github.com/mbauer83/effect-golang"
	"github.com/mbauer83/effect-golang/effecttest"
)

var inputs = []int{1, 2, 3, 4, 5, 6, 7, 8}

func doubling(tracker *effecttest.Tracker) func(int) effect.Effect[effect.Unit, string, int] {
	operations := effect.For[effect.Unit, string]()
	return func(value int) effect.Effect[effect.Unit, string, int] {
		return operations.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
			tracker.Record("visited")
			return effect.ExitSuccess[string](value * 2)
		})
	}
}

func TestForEachCollectsInInputOrder(t *testing.T) {
	tracker := &effecttest.Tracker{}
	exit := effect.Run(context.Background(), effect.Unit{}, effect.ForEach(inputs, doubling(tracker)))

	want := []int{2, 4, 6, 8, 10, 12, 14, 16}
	if got, ok := exit.Value(); !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, exit)
	}
}

func TestForEachShortCircuitsOnTheFirstFailure(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}

	program := effect.ForEach(inputs, func(value int) effect.Effect[effect.Unit, string, int] {
		return operations.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
			tracker.Record("visited")
			if value == 3 {
				return effect.ExitFailure[string, int]("rejected")
			}
			return effect.ExitSuccess[string](value)
		})
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if exit.IsSuccess() {
		t.Fatalf("expected a failure, got %v", exit)
	}
	if got := tracker.Count("visited"); got != 3 {
		t.Fatalf("expected the traversal to stop at the failure, visited %d", got)
	}
}

func TestForEachIsLazyAndDoesNotShareAccumulators(t *testing.T) {
	tracker := &effecttest.Tracker{}
	program := effect.ForEach(inputs, doubling(tracker))
	if got := tracker.Count("visited"); got != 0 {
		t.Fatalf("expected construction to run nothing, visited %d", got)
	}

	first := effect.Run(context.Background(), effect.Unit{}, program)
	second := effect.Run(context.Background(), effect.Unit{}, program)
	left, _ := first.Value()
	right, _ := second.Value()
	if !reflect.DeepEqual(left, right) || len(left) != len(inputs) {
		t.Fatalf("repeated interpretation diverged: %v then %v", left, right)
	}
}

func TestAllPreservesResultOrderWhileRunningConcurrently(t *testing.T) {
	meetingPoint := effecttest.NewBarrier(len(inputs))
	operations := effect.For[effect.Unit, string]()

	program := effect.ForEachPar(inputs, func(value int) effect.Effect[effect.Unit, string, int] {
		return operations.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
			meetingPoint.Arrive()
			return effect.ExitSuccess[string](value * 10)
		})
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	want := []int{10, 20, 30, 40, 50, 60, 70, 80}
	if got, ok := exit.Value(); !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("expected input order preserved, got %v", exit)
	}
}

func TestForEachParNBoundsSimultaneousBranches(t *testing.T) {
	const (
		limit    = 3
		branches = 12
	)
	runtime, clock := effecttest.NewTimedRuntime(t)
	operations := effect.For[effect.Unit, string]()
	work := make([]int, branches)

	// Every branch parks on the manual clock, so the number of registered
	// sleepers is exactly the number of branches currently in flight.
	program := effect.ForEachParN(work, limit, func(int) effect.Effect[effect.Unit, string, int] {
		return operations.Sleep(time.Second).As(1)
	})

	result := make(chan effect.Exit[string, []int], 1)
	finished := make(chan struct{})
	go func() {
		result <- runtime.Run(context.Background(), effect.Unit{}, program)
		close(finished)
	}()

	clock.AwaitSleepers(t, 1)
	overBound, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if err := clock.WaitForPending(overBound, limit+1); err == nil {
		t.Fatalf("expected at most %d branches in flight, saw more", limit)
	}

	drainParkedBranches(clock, finished)
	exit := <-result
	collected, ok := exit.Value()
	if !ok || len(collected) != branches {
		t.Fatalf("expected every branch to complete, got %v", exit)
	}
}

// drainParkedBranches advances the manual clock until the program finishes,
// releasing one bounded round of branches at a time.
func drainParkedBranches(clock *effecttest.ManualClock, finished <-chan struct{}) {
	for {
		select {
		case <-finished:
			return
		default:
		}
		if clock.PendingSleeps() > 0 {
			clock.Advance(time.Second)
		}
	}
}

func TestForEachParCancelsRemainingBranchesOnTheFirstFailure(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}
	work := effecttest.NewBlocker(tracker)

	program := effect.ForEachPar([]int{0, 1}, func(value int) forkedProgram {
		if value == 0 {
			return operations.From(func(context.Context, effect.Unit) effect.Exit[string, string] {
				work.AwaitStart()
				return effect.ExitFailure[string, string]("rejected")
			})
		}
		return effect.Scoped(func(scope effect.Scope) forkedProgram {
			return effecttest.TrackedResource[effect.Unit, string](scope, tracker, "branch-handle").AndThen(effecttest.Blocking[effect.Unit, string](work, "finished"))
		})
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected a failure, got %v", exit)
	}
	if failure, ok := cause.Failure(); !ok || failure != "rejected" {
		t.Fatalf("expected only the real failure, got %v", cause)
	}
	if got := tracker.Count("release branch-handle"); got != 1 {
		t.Fatalf("expected the canceled branch's resource released, got %d in %v", got, tracker.Events())
	}
}

func TestForEachParPreservesIndependentFailuresInInputOrder(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	meetingPoint := effecttest.NewBarrier(3)

	program := effect.ForEachPar([]string{"alpha", "beta", "gamma"},
		func(value string) forkedProgram {
			return operations.From(func(context.Context, effect.Unit) effect.Exit[string, string] {
				meetingPoint.Arrive()
				if value == "beta" {
					return effect.ExitSuccess[string](value)
				}
				return effect.ExitFailure[string, string](value + " failed")
			})
		},
	)

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected a failure, got %v", exit)
	}
	want := []string{"alpha failed", "gamma failed"}
	if got := cause.Failures(); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v in input order, got %v", want, got)
	}
}

func TestParallelTraversalOfNothingSucceedsWithNothing(t *testing.T) {
	program := effect.AllPar([]effect.Effect[effect.Unit, string, int]{})
	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if value, ok := exit.Value(); !ok || len(value) != 0 {
		t.Fatalf("expected an empty result, got %v", exit)
	}
}

func TestAllParCollectsEveryEffectBoundedOrNot(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	effects := []effect.Effect[effect.Unit, string, int]{
		operations.Succeed(1),
		operations.Succeed(2),
		operations.Succeed(3),
	}
	programs := map[string]effect.Effect[effect.Unit, string, []int]{
		"unbounded": effect.AllPar(effects),
		"bounded":   effect.AllParN(effects, 2),
	}

	for name, program := range programs {
		exit := effect.Run(context.Background(), effect.Unit{}, program)
		if got, ok := exit.Value(); !ok || !reflect.DeepEqual(got, []int{1, 2, 3}) {
			t.Fatalf("%s: unexpected exit: %v", name, exit)
		}
	}
}
