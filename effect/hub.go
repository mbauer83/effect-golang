package effect

import (
	"context"

	"github.com/mbauer83/effect-golang/effect/internal/lifetime"
)

// Hub broadcasts each published value to every current subscriber.
//
// This is the one thing a native Go channel cannot do at all. A channel
// delivers each value to exactly one receiver, so broadcasting over channels
// means fanning out by hand and getting per-subscriber backpressure wrong.
//
// Each subscriber owns an inbox, so a slow subscriber affects only itself under
// a dropping policy and applies backpressure to the publisher under a
// suspending one. Subscribers see values published after they subscribed: there
// is no replay, because keeping history would make the hub's memory a function
// of how long the program has run.
type Hub[A any] struct {
	state *lifetime.Hub[A]
}

// Subscription is one subscriber's view of a Hub.
//
// It deliberately cannot publish or shut anything down. A subscriber that could
// offer into its own inbox, or end delivery for everyone, would be able to
// break invariants that are not its to break.
type Subscription[A any] struct {
	hub   *lifetime.Hub[A]
	token uint64
	inbox *lifetime.Queue[A]
}

// NewHub creates a hub whose subscribers each get an inbox of the given
// capacity, behaving as whenFull says once that inbox is full.
//
// The caller owns its shutdown; prefer Scope.Hub, which ties shutdown to a
// lifetime and so cannot leave a subscriber waiting on a hub nobody will
// publish to again.
func NewHub[R, A any](capacity int, whenFull WhenFull) Effect[R, Never, Hub[A]] {
	return From(func(context.Context, R) Exit[Never, Hub[A]] {
		return ExitSuccess[Never](Hub[A]{
			state: lifetime.NewHub(capacity, fullQueuePolicy[A](whenFull)),
		})
	})
}

// Hub creates a hub that is shut down when this scope closes.
func (scope Scope) Hub[R, A any](capacity int, whenFull WhenFull) Effect[R, Never, Hub[A]] {
	return scope.AcquireRelease(
		NewHub[R, A](capacity, whenFull),
		func(hub Hub[A]) Effect[R, Never, Unit] {
			return hub.Shutdown[R]()
		},
	)
}

// Subscribe adds a subscriber for the lifetime of scope, so leaving the scope
// unsubscribes and shuts down that subscriber's inbox.
//
// A subscription is a resource rather than a value because forgetting to remove
// one is a leak: the hub would keep filling an inbox nobody reads, and under a
// suspending policy that eventually stops the publisher.
func (scope Scope) Subscribe[R, A any](hub Hub[A]) Effect[R, Never, Subscription[A]] {
	return scope.AcquireRelease(subscribe[R](hub), unsubscribe[R, A])
}

func subscribe[R, A any](hub Hub[A]) Effect[R, Never, Subscription[A]] {
	return From(func(context.Context, R) Exit[Never, Subscription[A]] {
		inbox, token, subscribed := hub.state.Subscribe()
		if !subscribed {
			return exitInterrupt[Never, Subscription[A]](lifetime.ErrHubShutdown)
		}
		return ExitSuccess[Never](Subscription[A]{hub: hub.state, token: token, inbox: inbox})
	})
}

// unsubscribe removes the subscriber from the hub and shuts its inbox down,
// so the hub stops filling an inbox nobody will read and anything still taking
// from it learns that the subscription has ended.
func unsubscribe[R, A any](subscription Subscription[A]) Effect[R, Never, Unit] {
	return From(func(context.Context, R) Exit[Never, Unit] {
		subscription.hub.Unsubscribe(subscription.token)
		return ExitSuccess[Never](Unit{})
	})
}

// Publish delivers value to every current subscriber. The result reports
// whether the hub accepted it, which a shut-down hub does not.
//
// Delivery is sequential in subscription order, so a suspending subscriber
// delays the ones after it. That is the price of backpressure: choose a
// dropping policy when one slow subscriber must not hold up the others.
func (hub Hub[A]) Publish[R any](value A) Effect[R, Never, bool] {
	return From(func(ctx context.Context, _ R) Exit[Never, bool] {
		accepted, interrupted := hub.state.Publish(ctx, value)
		if interrupted {
			return exitInterrupt[Never, bool](lifetime.CancellationReason(ctx))
		}
		return ExitSuccess[Never](accepted)
	})
}

// Shutdown stops the hub accepting values and shuts down every inbox, so each
// subscriber drains what it already has and then learns the hub is finished.
func (hub Hub[A]) Shutdown[R any]() Effect[R, Never, Unit] {
	return From(func(context.Context, R) Exit[Never, Unit] {
		hub.state.Shutdown()
		return ExitSuccess[Never](Unit{})
	})
}

// Subscribers reports how many inboxes the hub is delivering to.
func (hub Hub[A]) Subscribers() int {
	return hub.state.Subscribers()
}

// IsShutdown reports whether the hub has been shut down.
func (hub Hub[A]) IsShutdown() bool {
	return hub.state.IsShutdown()
}

// Take removes the next value from this subscription's inbox, waiting until one
// arrives. OK is false once the subscription has ended and its inbox is
// drained, which is the same signal a closed channel and a shut-down Queue give.
func (subscription Subscription[A]) Take[R any]() Effect[R, Never, Receive[A]] {
	return From(func(ctx context.Context, _ R) Exit[Never, Receive[A]] {
		value, ok, interrupted := subscription.inbox.Take(ctx)
		if interrupted {
			return exitInterrupt[Never, Receive[A]](lifetime.CancellationReason(ctx))
		}
		return ExitSuccess[Never](Receive[A]{Value: value, OK: ok})
	})
}

// TakeAvailable removes the values already waiting in this subscription's
// inbox, without blocking. An empty result means the inbox was empty at that
// instant, not that the subscription has ended.
func (subscription Subscription[A]) TakeAvailable[R any](limit int) Effect[R, Never, []A] {
	return From(func(context.Context, R) Exit[Never, []A] {
		return ExitSuccess[Never](subscription.inbox.TakeAvailable(limit))
	})
}

// TakeUpTo removes up to limit values, waiting for at least one. An empty
// result therefore means the subscription has ended and drained.
func (subscription Subscription[A]) TakeUpTo[R any](limit int) Effect[R, Never, []A] {
	return From(func(ctx context.Context, _ R) Exit[Never, []A] {
		batch, interrupted := subscription.inbox.TakeUpTo(ctx, limit)
		if interrupted {
			return exitInterrupt[Never, []A](lifetime.CancellationReason(ctx))
		}
		return ExitSuccess[Never](batch)
	})
}

// Pending reports how many values are waiting in this subscription's inbox.
func (subscription Subscription[A]) Pending() int {
	return subscription.inbox.Size()
}

// Operations carries these channels into the hub operations below, whose own
// requirement channel is unused and whose failure channel is Never.

// Hub creates a hub whose shutdown belongs to scope, in these channels.
func (Operations[R, E]) Hub[A any](scope Scope, capacity int, whenFull WhenFull) Effect[R, E, Hub[A]] {
	return WidenError[E](scope.Hub[R, A](capacity, whenFull))
}

// Subscribe adds a subscriber for the lifetime of scope, in these channels.
func (Operations[R, E]) Subscribe[A any](scope Scope, hub Hub[A]) Effect[R, E, Subscription[A]] {
	return WidenError[E](scope.Subscribe[R](hub))
}

// Publish delivers a value to every subscriber, in these channels.
func (Operations[R, E]) Publish[A any](hub Hub[A], value A) Effect[R, E, bool] {
	return WidenError[E](hub.Publish[R](value))
}

// Receive removes the next value from a subscription, in these channels.
func (Operations[R, E]) Receive[A any](subscription Subscription[A]) Effect[R, E, Receive[A]] {
	return WidenError[E](subscription.Take[R]())
}

// ReceiveUpTo removes a batch from a subscription in these channels, waiting
// for at least one value.
func (Operations[R, E]) ReceiveUpTo[A any](subscription Subscription[A], limit int) Effect[R, E, []A] {
	return WidenError[E](subscription.TakeUpTo[R](limit))
}

// ReceiveAvailable removes a subscription's waiting backlog without blocking,
// in these channels.
func (Operations[R, E]) ReceiveAvailable[A any](subscription Subscription[A], limit int) Effect[R, E, []A] {
	return WidenError[E](subscription.TakeAvailable[R](limit))
}
