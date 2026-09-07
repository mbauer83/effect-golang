package unit

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effecttest"
)

func TestDeferredIsObservedByEveryWaiter(t *testing.T) {
	const waiters = 32
	operations := effect.For[effect.Unit, string]()

	// A channel would hand the value to whoever received first. A Deferred
	// stores it and broadcasts, so every waiter sees the same outcome.
	program := effect.Scoped(func(effect.Scope) forkedProgram {
		return operations.Deferred[string]().FlatMap(
			func(pending effect.Deferred[string, string]) forkedProgram {
				var observers sync.WaitGroup
				seen := make([]string, waiters)
				for index := range waiters {
					observers.Go(func() {
						exit := effect.Run(context.Background(), effect.Unit{}, pending.Await[effect.Unit]())
						seen[index], _ = exit.Value()
					})
				}

				fulfilled := effect.Run(context.Background(), effect.Unit{},
					pending.Succeed[effect.Unit]("supplied"))
				observers.Wait()

				if won, _ := fulfilled.Value(); !won {
					t.Error("expected the first completion to report that it won")
				}
				for index, value := range seen {
					if value != "supplied" {
						t.Errorf("waiter %d saw %q", index, value)
					}
				}
				return operations.Succeed("all waiters agreed")
			},
		)
	})

	if exit := effect.Run(context.Background(), effect.Unit{}, program); exit.IsFailure() {
		t.Fatalf("unexpected failure: %v", exit)
	}
}

func TestDeferredIsFulfilledOnlyOnce(t *testing.T) {
	operations := effect.For[effect.Unit, string]()

	program := operations.Deferred[string]().FlatMap(
		func(pending effect.Deferred[string, string]) forkedProgram {
			first := effect.Run(context.Background(), effect.Unit{},
				pending.Succeed[effect.Unit]("first"))
			second := effect.Run(context.Background(), effect.Unit{},
				pending.Succeed[effect.Unit]("second"))

			if won, _ := first.Value(); !won {
				t.Error("expected the first writer to win")
			}
			if won, _ := second.Value(); won {
				t.Error("expected the second writer to lose")
			}
			return pending.Await[effect.Unit]()
		},
	)

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if value, ok := exit.Value(); !ok || value != "first" {
		t.Fatalf("expected the first value to stand, got %v", exit)
	}
}

func TestDeferredAdoptsATypedFailure(t *testing.T) {
	operations := effect.For[effect.Unit, string]()

	program := operations.Deferred[string]().FlatMap(
		func(pending effect.Deferred[string, string]) forkedProgram {
			effect.Run(context.Background(), effect.Unit{}, pending.Fail[effect.Unit]("rejected"))
			return pending.Await[effect.Unit]()
		},
	)

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected the failure to be adopted, got %v", exit)
	}
	if failure, ok := cause.Failure(); !ok || failure != "rejected" {
		t.Fatalf("unexpected cause: %v", cause)
	}
}

func TestDeferredAwaitIsInterruptible(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	stop := errors.New("caller stopped waiting")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(stop)

	program := operations.Deferred[string]().FlatMap(
		func(pending effect.Deferred[string, string]) forkedProgram {
			return pending.Await[effect.Unit]()
		},
	)

	exit := effect.Run(ctx, effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected interruption, got %v", exit)
	}
	if interruption, ok := cause.Interruption(); !ok || !errors.Is(interruption.Cause, stop) {
		t.Fatalf("expected the caller's cancellation cause, got %v", cause)
	}
}

func TestDeferredDoneInteroperatesWithAnOrdinarySelect(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}

	program := effect.Scoped(func(effect.Scope) forkedProgram {
		return operations.Deferred[string]().FlatMap(
			func(pending effect.Deferred[string, string]) forkedProgram {
				supply := operations.WidenError(pending.Succeed[effect.Unit]("ready").As("done"))
				return operations.Fork(supply).
					FlatMap(func(forkedFiber) forkedProgram {
						<-pending.Done()
						observed, ok := pending.Poll()
						if !ok {
							t.Error("expected Poll to report the fulfilled value")
						}
						value, _ := observed.Value()
						tracker.Record(value)
						return operations.Succeed(value)
					})
			},
		)
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if value, ok := exit.Value(); !ok || value != "ready" {
		t.Fatalf("unexpected exit: %v", exit)
	}
}
