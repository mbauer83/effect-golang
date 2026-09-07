# Hub reference

Reach for a native channel first. These types exist only where a channel
genuinely cannot do the job, which is the rule
[the channel reference](channels.md) states and these pages continue.

`Hub[A]` broadcasts each published value to every current subscriber.

This is the one case a channel cannot serve at all, rather than merely serve
awkwardly: a channel delivers each value to exactly one receiver, so
broadcasting over channels means fanning out by hand and getting per-subscriber
backpressure wrong.

```go
func NewHub[R, A any](capacity int, whenFull WhenFull) Effect[R, Never, Hub[A]]
func (scope Scope) Hub[R, A any](capacity int, whenFull WhenFull) Effect[R, Never, Hub[A]]
func (scope Scope) Subscribe[R, A any](hub Hub[A]) Effect[R, Never, Subscription[A]]

func (hub Hub[A]) Publish[R any](value A) Effect[R, Never, bool]
func (hub Hub[A]) Shutdown[R any]() Effect[R, Never, Unit]
func (hub Hub[A]) Subscribers() int
func (hub Hub[A]) IsShutdown() bool
```

## Inboxes

Each subscriber owns an inbox of the hub's capacity, behaving as its
[`WhenFull`](queue.md) choice says once that inbox is full. That is what makes
one slow subscriber's cost its own under a dropping policy, and the publisher's
under a suspending one.

Delivery is sequential in subscription order, so a suspending subscriber delays
the ones after it. That is the price of backpressure; choose a dropping policy
when one slow subscriber must not hold up the others.

`Publish` reports whether the hub accepted the value. A shut-down hub does not.
Whether an individual inbox kept it is that subscriber's business, decided by
the policy the hub was created with.

## No replay

A subscriber sees values published after it subscribed. There is no replay,
because keeping history would make the hub's memory a function of how long the
program has run. A late subscriber that needs earlier values needs a durable
log, which is a different thing with different ownership semantics.

## Subscription

```go
func (subscription Subscription[A]) Take[R any]() Effect[R, Never, Receive[A]]
func (subscription Subscription[A]) TakeUpTo[R any](limit int) Effect[R, Never, []A]
func (subscription Subscription[A]) TakeAvailable[R any](limit int) Effect[R, Never, []A]
func (subscription Subscription[A]) Pending() int
```

A `Subscription` deliberately cannot publish or shut anything down. A subscriber
able to offer into its own inbox, or to end delivery for everyone, could break
invariants that are not its to break.

`Take` reports `OK == false` once the subscription has ended and its inbox is
drained, which is the same signal a closed channel and a shut-down `Queue` give.
`TakeAvailable` never waits; see [batching](queue.md#batching) for which to use.

## Lifetimes

Subscribing is `Scope.Subscribe` and nothing else, because a subscription is a
resource: forgetting to remove one is a leak, and the hub would keep filling an
inbox nobody reads until a suspending policy stopped the publisher. Leaving the
scope removes the subscriber and shuts down its inbox, so anything still taking
from it learns the subscription has ended.

Prefer `Scope.Hub` over `NewHub` for the same reason `Scope.Queue` is preferred:
work forked inside the scope is released by the cancellation that closure
performs first, and the shutdown that follows tells whatever still holds the hub
that it is finished. Subscribing to a shut-down hub fails with an interruption
naming `ErrHubShutdown` rather than handing back an inbox nothing will fill.

## See also

- [Queue](queue.md), for point-to-point work with backpressure.
- [Deferred](deferred.md), for a single value every waiter observes.
