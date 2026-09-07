package lifetime

import (
	"context"

	"github.com/mbauer83/effect-golang/effect/internal/outcome"
)

// Fiber is the erased runtime state of one forked computation: an identity, a
// way to cancel it, and a completion every observer can read repeatedly.
type Fiber struct {
	id       uint64
	cancel   context.CancelCauseFunc
	terminal *Completion
}

// NewFiber creates a fiber that is not yet running.
func NewFiber(id uint64, cancel context.CancelCauseFunc) *Fiber {
	return &Fiber{id: id, cancel: cancel, terminal: NewCompletion()}
}

// ID returns the fiber's stable runtime-local identity.
func (fiber *Fiber) ID() uint64 {
	return fiber.id
}

// Done exposes the fiber's completion signal for use in an ordinary Go select.
func (fiber *Fiber) Done() <-chan struct{} {
	return fiber.terminal.Done()
}

// Complete records the fiber's terminal result exactly once.
func (fiber *Fiber) Complete(exit outcome.Exit) {
	fiber.terminal.Complete(exit)
}

// CompleteOnPanic terminates the fiber with a defect if the runtime's own fiber
// body panicked, so no observer can wait forever on a library bug.
func (fiber *Fiber) CompleteOnPanic() {
	fiber.terminal.CompleteOnPanic()
}

// Poll reports the terminal result when the fiber has already completed.
func (fiber *Fiber) Poll() (outcome.Exit, bool) {
	return fiber.terminal.Poll()
}

// Await blocks until the fiber completes or ctx is canceled. The bool reports
// whether the fiber completed; waiting is therefore itself interruptible.
func (fiber *Fiber) Await(ctx context.Context) (outcome.Exit, bool) {
	return fiber.terminal.Await(ctx)
}

// Wait blocks until the fiber completes and returns its terminal result.
func (fiber *Fiber) Wait() outcome.Exit {
	return fiber.terminal.Wait()
}

// Interrupt requests cancellation and waits for the fiber to terminate, which
// happens only after its own child scope has closed and released every resource
// it acquired. It deliberately does not select on the caller's context: a
// caller that asked for interruption is entitled to the final result, and
// returning early would leave cleanup unobserved.
func (fiber *Fiber) Interrupt(reason error) outcome.Exit {
	fiber.cancel(reason)
	return fiber.terminal.Wait()
}
