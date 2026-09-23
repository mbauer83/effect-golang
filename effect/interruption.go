package effect

import (
	"context"

	"github.com/mbauer83/effect-golang/effect/internal/lifetime"
)

// The runtime attaches one of these reasons whenever it cancels work, so an
// Interruption records why the work stopped instead of a generic
// context.Canceled. Compare them with errors.Is on an Interruption's Cause.
var (
	// ErrScopeClosed reports that the scope owning the work was closed.
	ErrScopeClosed = lifetime.ErrScopeClosed
	// ErrFiberInterrupted reports an explicit request to interrupt a fiber.
	ErrFiberInterrupted = lifetime.ErrFiberInterrupted
	// ErrSiblingFailed reports that a parallel sibling failed, which made this
	// branch's result unnecessary.
	ErrSiblingFailed = lifetime.ErrSiblingFailed
	// ErrRaceLost reports that another branch of a race completed first.
	ErrRaceLost = lifetime.ErrRaceLost
	// ErrTimedOut reports that a timeout elapsed before the work completed.
	ErrTimedOut = lifetime.ErrTimedOut
	// ErrRuntimeClosed reports that the owning Runtime was closed.
	ErrRuntimeClosed = lifetime.ErrRuntimeClosed
	// ErrHubShutdown reports that a Hub was shut down before a subscription
	// could be added.
	ErrHubShutdown = lifetime.ErrHubShutdown
)

// interruptionReason reports why the current context was canceled, or nil when
// it is still live. The explicit cancellation cause is preferred so a caller's
// reason survives instead of a generic context.Canceled.
func interruptionReason(ctx context.Context) error {
	if ctx.Err() == nil {
		return nil
	}
	return lifetime.CancellationReason(ctx)
}

// interruptExit reports an interruption exit when ctx is already canceled.
// Built-in effects that call a Go API which cannot itself be canceled use it as
// a checkpoint on both sides of the call.
func interruptExit[E, A any](ctx context.Context) (Exit[E, A], bool) {
	reason := interruptionReason(ctx)
	if reason == nil {
		return Exit[E, A]{}, false
	}
	return exitInterrupt[E, A](reason), true
}
