package unit

import (
	"context"
	"errors"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effecttest"
)

// These cases cover fork ownership and lifetime: which scope owns a child, what
// happens when that scope closes or is canceled, and what a closed scope does
// with a fork request.

func TestExplicitInterruptWaitsForChildFinalizers(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}
	work := effecttest.NewBlocker(tracker)

	child := effect.Scoped(func(inner effect.Scope) forkedProgram {
		return effecttest.TrackedResource[effect.Unit, string](inner, tracker, "child-handle").AndThen(effecttest.Blocking[effect.Unit, string](work, "finished"))
	})

	program := effect.Scoped(func(effect.Scope) forkedProgram {
		return operations.Fork(child).FlatMap(func(fiber forkedFiber) forkedProgram {
			work.AwaitStart()
			return operations.Interrupt(fiber).Map(
				func(terminal effect.Exit[string, string]) string {
					cause, failed := terminal.Cause()
					if !failed || !cause.IsInterruptedOnly() {
						t.Errorf("expected an interrupted child, got %v", terminal)
					}
					if got := tracker.Count("release child-handle"); got != 1 {
						t.Errorf("expected the child's resource released before Interrupt returned, got %d", got)
					}
					return "interrupted"
				},
			)
		})
	})

	if exit := effect.Run(context.Background(), effect.Unit{}, program); exit.IsFailure() {
		t.Fatalf("unexpected failure: %v", exit)
	}
}

func TestScopeClosureCancelsAndAwaitsItsChildren(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}
	work := effecttest.NewBlocker(tracker)

	program := effect.Scoped(func(effect.Scope) forkedProgram {
		return operations.Fork(effecttest.Blocking[effect.Unit, string](work, "finished")).FlatMap(func(forkedFiber) forkedProgram {
			work.AwaitStart()
			return operations.Succeed("body finished")
		})
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if value, ok := exit.Value(); !ok || value != "body finished" {
		t.Fatalf("unexpected exit: %v", exit)
	}
	if got := tracker.Count("interrupted"); got != 1 {
		t.Fatalf("expected the child to be interrupted by scope closure, got %d", got)
	}
}

func TestParentCancellationPreservesTheCancellationCause(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}
	work := effecttest.NewBlocker(tracker)
	stop := errors.New("shutdown requested")
	ctx, cancel := context.WithCancelCause(context.Background())

	var child forkedFiber
	program := effect.Scoped(func(effect.Scope) forkedProgram {
		return operations.Fork(effecttest.Blocking[effect.Unit, string](work, "finished")).FlatMap(func(fiber forkedFiber) forkedProgram {
			child = fiber
			work.AwaitStart()
			cancel(stop)
			return operations.Succeed("cancelled")
		})
	})

	effect.Run(ctx, effect.Unit{}, program)

	// Run returned, so the enclosing scope has already awaited the child.
	terminal, completed := child.Poll()
	if !completed {
		t.Fatal("expected scope closure to have awaited the child")
	}
	cause, failed := terminal.Cause()
	if !failed {
		t.Fatalf("expected the child to be interrupted, got %v", terminal)
	}
	interruption, ok := cause.Interruption()
	if !ok || !errors.Is(interruption.Cause, stop) {
		t.Fatalf("expected the caller's cancellation cause, got %v", cause)
	}
	if got := tracker.Count("interrupted"); got != 1 {
		t.Fatalf("expected the child to observe cancellation, got %d", got)
	}
}

func TestDeeplyNestedFibersAllTerminate(t *testing.T) {
	const depth = 200
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}

	var nest func(remaining int) forkedProgram
	nest = func(remaining int) forkedProgram {
		if remaining == 0 {
			return operations.From(func(context.Context, effect.Unit) effect.Exit[string, string] {
				tracker.Record("leaf")
				return effect.ExitSuccess[string]("leaf")
			})
		}
		return effect.Scoped(func(effect.Scope) forkedProgram {
			return operations.Fork(nest(remaining - 1)).FlatMap(operations.Join)
		})
	}

	exit := effect.Run(context.Background(), effect.Unit{}, nest(depth))
	if value, ok := exit.Value(); !ok || value != "leaf" {
		t.Fatalf("unexpected exit: %v", exit)
	}
	if got := tracker.Count("leaf"); got != 1 {
		t.Fatalf("expected exactly one leaf, got %d", got)
	}
}

func TestForkIntoClosedScopeStartsNoWork(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}

	var escaped effect.Scope
	effect.Run(context.Background(), effect.Unit{}, effect.Scoped(
		func(scope effect.Scope) forkedProgram {
			escaped = scope
			return operations.Succeed("opened")
		},
	))

	work := operations.From(func(context.Context, effect.Unit) effect.Exit[string, string] {
		tracker.Record("started")
		return effect.ExitSuccess[string]("ran")
	})
	exit := effect.Run(context.Background(), effect.Unit{}, operations.ForkIn(escaped, work))

	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected the closed lifetime to reject the fork, got %v", exit)
	}
	if interruption, ok := cause.Interruption(); !ok || !errors.Is(interruption.Cause, effect.ErrScopeClosed) {
		t.Fatalf("expected ErrScopeClosed, got %v", cause)
	}
	if got := tracker.Count("started"); got != 0 {
		t.Fatalf("expected no work to start, got %d", got)
	}
}
