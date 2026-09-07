package effect

import (
	"context"

	"github.com/mbauer83/effect-golang/internal/lifetime"
	runtimecore "github.com/mbauer83/effect-golang/internal/runtime"
)

// Fiber observes one forked computation. It is a handle: copying it is cheap
// and every copy observes the same computation.
//
// Await, Join and Poll are repeatable and safe for any number of concurrent
// observers, because a fiber stores its result and broadcasts completion by
// closing a channel rather than by sending its result once.
//
// The observation operations take their requirement channel as an explicit type
// argument, matching the other base capabilities: a fiber's own R was captured
// when it was forked, so observing it needs no environment of its own and must
// still compose inside a program that has one.
type Fiber[E, A any] struct {
	state *lifetime.Fiber
}

// ID returns the fiber's stable runtime-local identity, which also appears in
// its log records and lifecycle events.
func (fiber Fiber[E, A]) ID() uint64 {
	return fiber.state.ID()
}

// Done exposes completion for use in an ordinary Go select alongside contexts,
// timers and application channels. It is a synchronization signal and never
// yields the fiber's result.
func (fiber Fiber[E, A]) Done() <-chan struct{} {
	return fiber.state.Done()
}

// Poll returns the terminal result when the fiber has already completed.
func (fiber Fiber[E, A]) Poll() (Exit[E, A], bool) {
	exit, completed := fiber.state.Poll()
	if !completed {
		return Exit[E, A]{}, false
	}
	return Exit[E, A]{erased: exit}, true
}

// Await waits for termination and succeeds with the complete outcome, so the
// caller can inspect a typed failure, a defect or an interruption without
// adopting it. Waiting is itself interruptible.
func (fiber Fiber[E, A]) Await[R any]() Effect[R, Never, Exit[E, A]] {
	return fromRuntime(func(ctx context.Context, _ *runtimecore.State, _ R) Exit[Never, Exit[E, A]] {
		exit, completed := fiber.state.Await(ctx)
		if !completed {
			return exitInterrupted[Never, Exit[E, A]](lifetime.CancellationReason(ctx))
		}
		return ExitSuccess[Never](Exit[E, A]{erased: exit})
	})
}

// Join waits for termination and adopts the child's outcome: a typed child
// failure becomes a typed caller failure, a defect stays a defect and an
// interruption stays an interruption.
func (fiber Fiber[E, A]) Join[R any]() Effect[R, E, A] {
	return fromRuntime(func(ctx context.Context, _ *runtimecore.State, _ R) Exit[E, A] {
		exit, completed := fiber.state.Await(ctx)
		if !completed {
			return exitInterrupted[E, A](lifetime.CancellationReason(ctx))
		}
		return Exit[E, A]{erased: exit}
	})
}

// Interrupt requests cancellation and returns only once the fiber has
// terminated, its child scopes have closed and its resource finalizers have
// run. It is deliberately not a fire-and-forget cancellation: returning early
// would leave the fiber's cleanup unobserved.
func (fiber Fiber[E, A]) Interrupt[R any]() Effect[R, Never, Exit[E, A]] {
	return fromRuntime(func(_ context.Context, _ *runtimecore.State, _ R) Exit[Never, Exit[E, A]] {
		terminal := fiber.state.Interrupt(lifetime.ErrFiberInterrupted)
		return ExitSuccess[Never](Exit[E, A]{erased: terminal})
	})
}
