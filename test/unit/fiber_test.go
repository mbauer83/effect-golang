package unit

import (
	"context"
	"sync"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effecttest"
)

func TestManySimultaneousAwaitsObserveOneResult(t *testing.T) {
	const observers = 64
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}
	work := effecttest.NewBlocker(tracker)

	program := effect.Scoped(func(effect.Scope) forkedProgram {
		return operations.Fork(effecttest.Blocking[effect.Unit, string](work, "finished")).FlatMap(
			func(fiber forkedFiber) forkedProgram {
				work.Release()

				var waiters sync.WaitGroup
				results := make([]effect.Exit[string, string], observers)
				for index := range observers {
					waiters.Go(func() {
						exit := effect.Run(context.Background(), effect.Unit{}, fiber.Await[effect.Unit]())
						results[index], _ = exit.Value()
					})
				}
				waiters.Wait()

				for index, observed := range results {
					if value, ok := observed.Value(); !ok || value != "finished" {
						t.Errorf("observer %d saw %v", index, observed)
					}
				}
				return operations.Succeed("all observers agreed")
			},
		)
	})

	if exit := effect.Run(context.Background(), effect.Unit{}, program); exit.IsFailure() {
		t.Fatalf("unexpected failure: %v", exit)
	}
	if got := tracker.Count("completed"); got != 1 {
		t.Fatalf("expected the body to run once, got %d", got)
	}
}

func TestManySimultaneousJoinsAdoptTheSameFailure(t *testing.T) {
	const observers = 32
	operations := effect.For[effect.Unit, string]()

	program := effect.Scoped(func(effect.Scope) forkedProgram {
		return operations.Fork(operations.Fail[string]("rejected")).FlatMap(
			func(fiber forkedFiber) forkedProgram {
				var waiters sync.WaitGroup
				failures := make([]string, observers)
				for index := range observers {
					waiters.Go(func() {
						exit := effect.Run(context.Background(), effect.Unit{}, fiber.Join[effect.Unit]())
						cause, _ := exit.Cause()
						failures[index], _ = cause.Failure()
					})
				}
				waiters.Wait()

				for index, failure := range failures {
					if failure != "rejected" {
						t.Errorf("observer %d adopted %q", index, failure)
					}
				}
				return operations.Succeed("all observers agreed")
			},
		)
	})

	if exit := effect.Run(context.Background(), effect.Unit{}, program); exit.IsFailure() {
		t.Fatalf("unexpected failure: %v", exit)
	}
}

func TestPollReportsCompletionBeforeAndAfterTermination(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}
	work := effecttest.NewBlocker(tracker)

	program := effect.Scoped(func(effect.Scope) forkedProgram {
		return operations.Fork(effecttest.Blocking[effect.Unit, string](work, "finished")).FlatMap(
			func(fiber forkedFiber) forkedProgram {
				work.AwaitStart()
				if _, completed := fiber.Poll(); completed {
					t.Error("expected Poll to report an incomplete fiber")
				}
				work.Release()
				<-fiber.Done()

				exit, completed := fiber.Poll()
				if !completed {
					t.Error("expected Poll to report a completed fiber")
				}
				if value, ok := exit.Value(); !ok || value != "finished" {
					t.Errorf("unexpected polled result: %v", exit)
				}
				return operations.Succeed("polled")
			},
		)
	})

	if exit := effect.Run(context.Background(), effect.Unit{}, program); exit.IsFailure() {
		t.Fatalf("unexpected failure: %v", exit)
	}
}
