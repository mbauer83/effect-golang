package unit

import (
	"context"
	"slices"
	"testing"

	effect "github.com/mbauer83/effect-golang"
	"github.com/mbauer83/effect-golang/effecttest"
)

func TestChildrenTerminateBeforeScopeResourcesAreReleased(t *testing.T) {
	// The child uses a resource its own scope owns. Closing the scope must
	// cancel and await the child before releasing what the child was using;
	// releasing first would hand a live fiber a dead resource.
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}
	work := effecttest.NewBlocker(tracker)

	program := effect.Scoped(func(scope effect.Scope) forkedProgram {
		return effecttest.TrackedResource[effect.Unit, string](scope, tracker, "shared").FlatMap(func(string) forkedProgram {
			return operations.Fork(effecttest.Blocking[effect.Unit, string](work, "finished")).FlatMap(func(forkedFiber) forkedProgram {
				work.AwaitStart()
				return operations.Succeed("body finished")
			})
		})
	})

	if exit := effect.Run(context.Background(), effect.Unit{}, program); exit.IsFailure() {
		t.Fatalf("unexpected failure: %v", exit)
	}

	events := tracker.Events()
	interrupted := slices.Index(events, "interrupted")
	released := slices.Index(events, "release shared")
	if interrupted < 0 || released < 0 {
		t.Fatalf("expected both a child interruption and a release, got %v", events)
	}
	if interrupted > released {
		t.Fatalf("expected the child to terminate before its scope's resource was released, got %v", events)
	}
}
