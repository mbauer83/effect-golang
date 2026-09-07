package effect

import (
	"context"

	runtimecore "github.com/mbauer83/effect-golang/internal/runtime"
)

// The runtime attaches one of these reasons whenever it cancels work, so an
// Interruption records why the work stopped instead of a generic
// context.Canceled. Compare them with errors.Is on an Interruption's Cause.
var (
	// ErrScopeClosed reports that the scope owning the work was closed.
	ErrScopeClosed = runtimecore.ErrScopeClosed
	// ErrFiberInterrupted reports an explicit request to interrupt a fiber.
	ErrFiberInterrupted = runtimecore.ErrFiberInterrupted
	// ErrSiblingFailed reports that a parallel sibling failed, which made this
	// branch's result unnecessary.
	ErrSiblingFailed = runtimecore.ErrSiblingFailed
	// ErrRaceLost reports that another branch of a race completed first.
	ErrRaceLost = runtimecore.ErrRaceLost
	// ErrTimedOut reports that a timeout elapsed before the work completed.
	ErrTimedOut = runtimecore.ErrTimedOut
	// ErrRuntimeClosed reports that the owning Runtime was closed.
	ErrRuntimeClosed = runtimecore.ErrRuntimeClosed
)

// interruptionReason reports why the current context was canceled, or nil when
// it is still live. The explicit cancellation cause is preferred so a caller's
// reason survives instead of a generic context.Canceled.
func interruptionReason(ctx context.Context) error {
	if ctx.Err() == nil {
		return nil
	}
	return runtimecore.CancellationReason(ctx)
}

// interruptedExit reports an interruption exit when ctx is already canceled.
// Built-in effects that call a Go API which cannot itself be canceled use it as
// a checkpoint on both sides of the call.
func interruptedExit[E, A any](ctx context.Context) (Exit[E, A], bool) {
	reason := interruptionReason(ctx)
	if reason == nil {
		return Exit[E, A]{}, false
	}
	return exitInterrupted[E, A](reason), true
}
