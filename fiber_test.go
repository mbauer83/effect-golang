package effect_test

import (
	"context"
	"sync"
	"testing"

	effect "github.com/mbauer83/effect-golang"
	"github.com/mbauer83/effect-golang/effecttest"
)

type forkedProgram = effect.Effect[effect.Unit, string, string]
type forkedFiber = effect.Fiber[string, string]

// blocker is a cooperative unit of work that reports when it has really begun.
//
// Interruption tests must observe the work running before they cancel it:
// cancelling first is also correct runtime behaviour -- the work simply never
// starts -- but it would not exercise interruption.
type blocker struct {
	started chan struct{}
	release chan struct{}
	tracker *effecttest.Tracker
}

func newBlocker(tracker *effecttest.Tracker) *blocker {
	return &blocker{
		started: make(chan struct{}),
		release: make(chan struct{}),
		tracker: tracker,
	}
}

func (work *blocker) program() forkedProgram {
	return effect.For[effect.Unit, string]().From(
		func(ctx context.Context, _ effect.Unit) effect.Exit[string, string] {
			close(work.started)
			select {
			case <-work.release:
				work.tracker.Record("completed")
				return effect.ExitSuccess[string]("finished")
			case <-ctx.Done():
				work.tracker.Record("interrupted")
				return effect.ExitCause[string, string](
					effect.InterruptCause[string](context.Cause(ctx)),
				)
			}
		},
	)
}

// awaitStart blocks until the work has begun. It is ordinary Go code inside an
// effect callback, which is exactly how the runtime expects a program to
// interoperate with channels it does not own.
func (work *blocker) awaitStart() {
	<-work.started
}

func TestManySimultaneousAwaitsObserveOneResult(t *testing.T) {
	const observers = 64
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}
	work := newBlocker(tracker)

	program := effect.Scoped(func(effect.Scope) forkedProgram {
		return operations.Fork(work.program()).FlatMap(
			func(fiber forkedFiber) forkedProgram {
				close(work.release)

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
	work := newBlocker(tracker)

	program := effect.Scoped(func(effect.Scope) forkedProgram {
		return operations.Fork(work.program()).FlatMap(
			func(fiber forkedFiber) forkedProgram {
				work.awaitStart()
				if _, completed := fiber.Poll(); completed {
					t.Error("expected Poll to report an incomplete fiber")
				}
				close(work.release)
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
