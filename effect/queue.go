package effect

import (
	"context"

	"github.com/mbauer83/effect-golang/effect/internal/lifetime"
)

// Queue is a work queue for the cases a native Go channel cannot cover.
//
// Reach for a channel first. A Queue is worth its extra surface when you need
// one of three things a channel does not give you:
//
//   - shutdown that is safe from any side. Closing a channel is a
//     single-producer protocol; with several producers it is unsafe by
//     construction, and Go offers no race-free way to make it safe.
//   - a choice of what happens when it is full. A channel suspends, and that
//     is the only option.
//   - batched taking, so a consumer can amortise its per-item work.
//
// Everything else stays channel-shaped: values come out in the order they went
// in, and a shut-down queue drains before it reports that it is finished, which
// is the same signal a closed channel gives.
type Queue[A any] struct {
	state *lifetime.Queue[A]
}

// WhenFull chooses what a bounded queue does with a value it has no room for.
// It is required rather than defaulted, because the wrong answer here is the
// usual cause of a stalled or a lossy pipeline and a default would hide it.
type WhenFull uint8

const (
	// SuspendWhenFull waits for room, applying backpressure to the producer.
	SuspendWhenFull WhenFull = iota
	// DropNewestWhenFull refuses the incoming value, keeping the backlog.
	DropNewestWhenFull
	// DropOldestWhenFull discards the oldest queued value to make room, which
	// is what a queue of current-state updates wants.
	DropOldestWhenFull
)

// NewQueue creates a bounded queue. The caller owns its shutdown; prefer
// Scope.Queue, which ties shutdown to a lifetime and so cannot leave a consumer
// waiting on a queue nobody will ever fill.
func NewQueue[R, A any](capacity int, whenFull WhenFull) Effect[R, Never, Queue[A]] {
	return From(func(context.Context, R) Exit[Never, Queue[A]] {
		return ExitSuccess[Never](Queue[A]{state: lifetime.NewQueue(capacity, fullQueuePolicy[A](whenFull))})
	})
}

// NewUnboundedQueue creates a queue that never refuses a value. Its memory is
// bounded only by its producers, so it is a deliberate choice and not a
// convenience.
func NewUnboundedQueue[R, A any]() Effect[R, Never, Queue[A]] {
	return From(func(context.Context, R) Exit[Never, Queue[A]] {
		return ExitSuccess[Never](Queue[A]{state: lifetime.NewUnboundedQueue[A]()})
	})
}

// Queue creates a bounded queue that is shut down when this scope closes.
//
// Work forked inside the scope is released by the cancellation that closure
// performs first; the shutdown that follows is for anything still holding the
// queue afterwards, which learns the queue is finished instead of blocking on
// it forever. Prefer this form unless the queue is meant to outlive the scope
// that filled it.
func (scope Scope) Queue[R, A any](capacity int, whenFull WhenFull) Effect[R, Never, Queue[A]] {
	return acquireQueue[R](scope, NewQueue[R, A](capacity, whenFull))
}

// UnboundedQueue creates an unbounded queue bound to this scope's lifetime.
func (scope Scope) UnboundedQueue[R, A any]() Effect[R, Never, Queue[A]] {
	return acquireQueue[R](scope, NewUnboundedQueue[R, A]())
}

func acquireQueue[R, A any](scope Scope, create Effect[R, Never, Queue[A]]) Effect[R, Never, Queue[A]] {
	return scope.AcquireRelease(create, func(queue Queue[A]) Effect[R, Never, Unit] {
		return queue.Shutdown[R]()
	})
}

// fullQueuePolicy turns the caller's choice into the strategy the queue runs.
// The choice is a named value at the call site and a separate type inside, so
// neither the API nor the implementation carries a mode flag.
func fullQueuePolicy[A any](whenFull WhenFull) lifetime.FullQueuePolicy[A] {
	switch whenFull {
	case DropNewestWhenFull:
		return lifetime.DropNewest[A]()
	case DropOldestWhenFull:
		return lifetime.DropOldest[A]()
	default:
		return lifetime.BackPressure[A]()
	}
}

// Offer adds a value. The result reports whether the queue accepted it: a
// dropping queue may decline, and a shut-down queue always does.
func (queue Queue[A]) Offer[R any](value A) Effect[R, Never, bool] {
	return From(func(ctx context.Context, _ R) Exit[Never, bool] {
		accepted, interrupted := queue.state.Offer(ctx, value)
		if interrupted {
			return exitInterrupt[Never, bool](lifetime.CancellationReason(ctx))
		}
		return ExitSuccess[Never](accepted)
	})
}

// Take removes the next value, waiting until one arrives. OK is false only once
// the queue has been shut down and drained, which mirrors a closed channel and
// is why the result reuses Receive.
func (queue Queue[A]) Take[R any]() Effect[R, Never, Receive[A]] {
	return From(func(ctx context.Context, _ R) Exit[Never, Receive[A]] {
		value, ok, interrupted := queue.state.Take(ctx)
		if interrupted {
			return exitInterrupt[Never, Receive[A]](lifetime.CancellationReason(ctx))
		}
		return ExitSuccess[Never](Receive[A]{Value: value, OK: ok})
	})
}

// TakeAvailable removes up to limit values that are already waiting, without
// blocking. An empty result means the queue was empty at that instant, which is
// a different statement from being finished: use it to drain a backlog, and
// TakeUpTo to consume a stream.
func (queue Queue[A]) TakeAvailable[R any](limit int) Effect[R, Never, []A] {
	return From(func(context.Context, R) Exit[Never, []A] {
		return ExitSuccess[Never](queue.state.TakeAvailable(limit))
	})
}

// TakeUpTo removes up to limit values, waiting for at least one. An empty
// result therefore means the queue has been shut down and drained.
func (queue Queue[A]) TakeUpTo[R any](limit int) Effect[R, Never, []A] {
	return From(func(ctx context.Context, _ R) Exit[Never, []A] {
		batch, interrupted := queue.state.TakeUpTo(ctx, limit)
		if interrupted {
			return exitInterrupt[Never, []A](lifetime.CancellationReason(ctx))
		}
		return ExitSuccess[Never](batch)
	})
}

// Shutdown stops the queue accepting values and releases everyone waiting on
// it. It is safe from any side and idempotent.
func (queue Queue[A]) Shutdown[R any]() Effect[R, Never, Unit] {
	return From(func(context.Context, R) Exit[Never, Unit] {
		queue.state.Shutdown()
		return ExitSuccess[Never](Unit{})
	})
}

// Size reports how many values are waiting to be taken.
func (queue Queue[A]) Size() int {
	return queue.state.Size()
}

// IsShutdown reports whether the queue has been shut down.
func (queue Queue[A]) IsShutdown() bool {
	return queue.state.IsShutdown()
}

// Operations carries these channels into the operations below, whose own
// requirement channel is unused and whose failure channel is Never. Selecting
// the channels once keeps a program composable with FlatMap instead of forcing
// a widening at every step. The precise, narrower forms remain available.

// Queue creates a bounded queue in these channels.
func (Operations[R, E]) Queue[A any](capacity int, whenFull WhenFull) Effect[R, E, Queue[A]] {
	return WidenError[E](NewQueue[R, A](capacity, whenFull))
}

// ScopedQueue creates a bounded queue whose shutdown belongs to scope.
func (Operations[R, E]) ScopedQueue[A any](
	scope Scope,
	capacity int,
	whenFull WhenFull,
) Effect[R, E, Queue[A]] {
	return WidenError[E](scope.Queue[R, A](capacity, whenFull))
}

// Offer adds a value to a queue in these channels.
func (Operations[R, E]) Offer[A any](queue Queue[A], value A) Effect[R, E, bool] {
	return WidenError[E](queue.Offer[R](value))
}

// Take removes the next value from a queue in these channels.
func (Operations[R, E]) Take[A any](queue Queue[A]) Effect[R, E, Receive[A]] {
	return WidenError[E](queue.Take[R]())
}

// TakeUpTo removes a batch from a queue in these channels, waiting for at least
// one value.
func (Operations[R, E]) TakeUpTo[A any](queue Queue[A], limit int) Effect[R, E, []A] {
	return WidenError[E](queue.TakeUpTo[R](limit))
}

// TakeAvailable removes a queue's waiting backlog without blocking, in these
// channels.
func (Operations[R, E]) TakeAvailable[A any](queue Queue[A], limit int) Effect[R, E, []A] {
	return WidenError[E](queue.TakeAvailable[R](limit))
}
