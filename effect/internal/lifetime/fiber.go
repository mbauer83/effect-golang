package lifetime

import (
	"context"
	"github.com/mbauer83/effect-golang/effect/internal/outcome"
	"sync"
)

// Fiber is the erased runtime state of one forked computation.
//
// The result is written before done is closed, and every waiter reads it only
// after receiving from done. Closing a channel is a broadcast that happens
// before any receive observing it, so the result needs no lock and Await, Join
// and Poll are repeatable by arbitrarily many observers. A result channel would
// instead be consumed by the first receiver.
type Fiber struct {
	id        uint64
	done      chan struct{}
	cancel    context.CancelCauseFunc
	completed sync.Once
	result    outcome.Exit
}

// NewFiber creates a fiber that is not yet running.
func NewFiber(id uint64, cancel context.CancelCauseFunc) *Fiber {
	return &Fiber{id: id, done: make(chan struct{}), cancel: cancel}
}

// ID returns the fiber's stable runtime-local identity.
func (fiber *Fiber) ID() uint64 {
	return fiber.id
}

// Done exposes the fiber's completion signal for use in an ordinary Go select.
// It is a synchronization signal, not a consumable result channel.
func (fiber *Fiber) Done() <-chan struct{} {
	return fiber.done
}

// Complete records the fiber's terminal result exactly once.
func (fiber *Fiber) Complete(exit outcome.Exit) {
	fiber.completed.Do(func() {
		fiber.result = exit
		close(fiber.done)
	})
}

// CompleteOnPanic terminates the fiber with a defect if the runtime's own fiber
// body panicked, so no observer can wait forever on a library bug.
func (fiber *Fiber) CompleteOnPanic() {
	if recovered := recover(); recovered != nil {
		fiber.Complete(outcome.Failure(outcome.DieCause(outcome.CapturedDefect(recovered))))
	}
}

// Poll reports the terminal result when the fiber has already completed.
func (fiber *Fiber) Poll() (outcome.Exit, bool) {
	select {
	case <-fiber.done:
		return fiber.result, true
	default:
		return outcome.Exit{}, false
	}
}

// Await blocks until the fiber completes or ctx is canceled. The bool reports
// whether the fiber completed; waiting is therefore itself interruptible.
func (fiber *Fiber) Await(ctx context.Context) (outcome.Exit, bool) {
	select {
	case <-fiber.done:
		return fiber.result, true
	case <-ctx.Done():
		return outcome.Exit{}, false
	}
}

// Wait blocks until the fiber completes and returns its terminal result. It is
// deliberately not interruptible: a composition that has already decided to
// discard a branch must still observe that branch's cleanup.
func (fiber *Fiber) Wait() outcome.Exit {
	<-fiber.done
	return fiber.result
}

// Interrupt requests cancellation and waits for the fiber to terminate, which
// happens only after its own child scope has closed and released every resource
// it acquired. It deliberately does not select on the caller's context: a
// caller that asked for interruption is entitled to the final result, and
// returning early would leave cleanup unobserved.
func (fiber *Fiber) Interrupt(reason error) outcome.Exit {
	fiber.cancel(reason)
	<-fiber.done
	return fiber.result
}
