package runtime

import "errors"

// Cancellation reasons are attached with context.WithCancelCause so an
// interruption records why work stopped instead of a generic context.Canceled.
// They are comparable with errors.Is on the Interruption's cause.
var (
	// ErrScopeClosed reports that the scope owning the work was closed.
	ErrScopeClosed = errors.New("effect: scope closed")
	// ErrFiberInterrupted reports an explicit request to interrupt a fiber.
	ErrFiberInterrupted = errors.New("effect: fiber interrupted")
	// ErrSiblingFailed reports that a parallel sibling failed, making this
	// branch's result unnecessary.
	ErrSiblingFailed = errors.New("effect: parallel sibling failed")
	// ErrRaceLost reports that another branch of a race completed first.
	ErrRaceLost = errors.New("effect: race lost")
	// ErrTimedOut reports that a timeout elapsed before the work completed.
	ErrTimedOut = errors.New("effect: timed out")
	// ErrRuntimeClosed reports that the owning runtime was closed.
	ErrRuntimeClosed = errors.New("effect: runtime closed")
)
