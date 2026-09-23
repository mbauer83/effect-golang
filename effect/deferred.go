package effect

import (
	"context"

	"github.com/mbauer83/effect-golang/effect/internal/lifetime"
)

// Deferred is a value that will be supplied once and can be observed any number
// of times, by any number of waiters.
//
// It is the synchronization primitive a native channel cannot be: a channel
// hands a value to whoever receives first, whereas a Deferred stores its result
// and broadcasts, so every waiter sees the same outcome. Fiber completion has
// the same shape and shares the same mechanism.
//
// A Deferred is created by an effect rather than by a constructor, so it cannot
// exist before interpretation and cannot be shared accidentally between runs.
type Deferred[E, A any] struct {
	state *lifetime.Completion
}

// NewDeferred creates an unfulfilled Deferred.
func NewDeferred[R, E, A any]() Effect[R, Never, Deferred[E, A]] {
	return From(func(context.Context, R) Exit[Never, Deferred[E, A]] {
		return ExitSuccess[Never](Deferred[E, A]{state: lifetime.NewCompletion()})
	})
}

// Succeed supplies the value. The result reports whether this call was the one
// that fulfilled it, so a caller can tell whether it won the race.
func (deferred Deferred[E, A]) Succeed[R any](value A) Effect[R, Never, bool] {
	return deferred.Complete[R](ExitSuccess[E](value))
}

// Fail fulfils the Deferred with a typed failure.
func (deferred Deferred[E, A]) Fail[R any](failure E) Effect[R, Never, bool] {
	return deferred.Complete[R](ExitFailure[E, A](failure))
}

// Complete fulfils the Deferred with a complete outcome, which is how a defect
// or an interruption is handed to its waiters rather than being lost.
func (deferred Deferred[E, A]) Complete[R any](exit Exit[E, A]) Effect[R, Never, bool] {
	return From(func(context.Context, R) Exit[Never, bool] {
		return ExitSuccess[Never](deferred.state.Complete(exit.erased))
	})
}

// Await waits for the value and adopts its outcome: a typed failure becomes the
// caller's typed failure, a defect stays a defect, an interruption stays an
// interruption. Waiting is itself interruptible.
func (deferred Deferred[E, A]) Await[R any]() Effect[R, E, A] {
	return From(func(ctx context.Context, _ R) Exit[E, A] {
		result, ok := deferred.state.Await(ctx)
		if !ok {
			return exitInterrupt[E, A](lifetime.CancellationReason(ctx))
		}
		return Exit[E, A]{erased: result}
	})
}

// Poll returns the outcome when the Deferred has already been fulfilled.
func (deferred Deferred[E, A]) Poll() (Exit[E, A], bool) {
	fulfilled, ok := deferred.state.Poll()
	if !ok {
		return Exit[E, A]{}, false
	}
	return Exit[E, A]{erased: fulfilled}, true
}

// Done exposes fulfilment for use in an ordinary Go select alongside contexts,
// timers and application channels. It never yields the value; read that with
// Poll.
func (deferred Deferred[E, A]) Done() <-chan struct{} {
	return deferred.state.Done()
}

// Operations carries these channels into the operations below, whose own
// requirement channel is unused and whose failure channel is Never. Selecting
// the channels once keeps a program composable with FlatMap instead of forcing
// a widening at every step. The precise, narrower forms remain available.

// Deferred creates an unfulfilled Deferred in these channels.
func (Operations[R, E]) Deferred[A any]() Effect[R, E, Deferred[E, A]] {
	return WidenError[E](NewDeferred[R, E, A]())
}
