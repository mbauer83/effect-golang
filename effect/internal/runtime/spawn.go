package runtime

import (
	"context"
	"github.com/mbauer83/effect-golang/effect/capability"
	"github.com/mbauer83/effect-golang/effect/internal/lifetime"
	"github.com/mbauer83/effect-golang/effect/internal/outcome"
)

// StartFiber runs work on a new goroutine owned by owner.
//
// The child receives its own cancelable context and its own scope, so its
// resources and descendants are released before it completes and can never
// outlive it. Because owner tracks the goroutine, no library goroutine is
// without a lifetime owner. The bool reports whether owner accepted ownership;
// a rejected caller must not start the work at all.
func StartFiber(
	owner *lifetime.Scope,
	parent context.Context,
	state *State,
	work func(context.Context, *State) outcome.Exit,
) (*lifetime.Fiber, bool) {
	childCtx, cancel := context.WithCancelCause(parent)
	child := lifetime.NewScope(childCtx)
	fiber := lifetime.NewFiber(state.NextFiberID(), cancel)
	childState := state.Forked(child, fiber.ID())

	// The ledger is credited before the goroutine can be scheduled, so a
	// snapshot can never observe a completion without its start.
	ledger := state.Ledger()
	ledger.FiberStarted()
	started := owner.Fork(func() {
		defer fiber.CompleteOnPanic()
		defer cancel(lifetime.ErrFiberInterrupted)
		defer ledger.FiberCompleted()

		childCtx := child.Context()
		startedAt := childState.EmitStart(childCtx, capability.EventFiberStarted)
		exit := work(childCtx, childState)
		cleanup := child.Close(childCtx, exit, lifetime.ErrScopeClosed)
		if !cleanup.IsEmpty() {
			exit = outcome.Failure(exit.Cause().Then(cleanup))
		}
		childState.EmitEnd(childCtx, capability.EventFiberCompleted, startedAt, outcome.ExitStatus(exit))
		fiber.Complete(exit)
	})
	if !started {
		ledger.FiberCompleted()
		cancel(lifetime.ErrScopeClosed)
		return nil, false
	}
	return fiber, true
}
