package lifetime

import (
	"context"
	"sync"

	"github.com/mbauer83/effect-golang/effect/internal/outcome"
)

// Completion is a write-once result with repeatable broadcast observation.
//
// The result is written before done is closed, and every observer reads it only
// after receiving from done. Closing a channel is a broadcast that happens
// before any receive observing it, so the result needs no lock and any number
// of observers can read it any number of times. A result channel would instead
// be consumed by whoever received first.
//
// A fiber's terminal exit, a deferred value and a queue's handoff to a waiting
// taker are all this same shape, so they are all this type.
type Completion struct {
	done   chan struct{}
	once   sync.Once
	result outcome.Exit
}

func NewCompletion() *Completion {
	return &Completion{done: make(chan struct{})}
}

// Done exposes the completion signal for use in an ordinary Go select. It is a
// synchronization signal, not a consumable result channel.
func (completion *Completion) Done() <-chan struct{} {
	return completion.done
}

// Complete records the result. The bool reports whether this call was the one
// that completed it, so a caller can tell whether it won the race.
func (completion *Completion) Complete(exit outcome.Exit) bool {
	first := false
	completion.once.Do(func() {
		completion.result = exit
		close(completion.done)
		first = true
	})
	return first
}

// CompleteOnPanic completes with a defect if the code producing the result
// panicked, so no observer can wait forever on a library bug.
func (completion *Completion) CompleteOnPanic() {
	if recovered := recover(); recovered != nil {
		completion.Complete(outcome.Failure(outcome.DieCause(outcome.CaptureDefect(recovered))))
	}
}

// Poll reports the result when it has already been written.
func (completion *Completion) Poll() (outcome.Exit, bool) {
	select {
	case <-completion.done:
		return completion.result, true
	default:
		return outcome.Exit{}, false
	}
}

// Await blocks until the result is written or ctx is canceled. The bool reports
// whether it was written, so waiting is itself interruptible.
func (completion *Completion) Await(ctx context.Context) (outcome.Exit, bool) {
	select {
	case <-completion.done:
		return completion.result, true
	case <-ctx.Done():
		return outcome.Exit{}, false
	}
}

// Wait blocks until the result is written. It is deliberately not
// interruptible: a caller that has already discarded work must still observe
// that work's cleanup.
func (completion *Completion) Wait() outcome.Exit {
	<-completion.done
	return completion.result
}
