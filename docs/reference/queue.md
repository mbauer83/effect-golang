# Queue reference

Reach for a native channel first. These types exist only where a channel
genuinely cannot do the job, which is the rule
[the channel reference](channels.md) states and these pages continue.

`Queue[A]` is a work queue. Three things justify it over a channel:

| Need | Why a channel cannot |
|---|---|
| shutdown that is safe from any side | `close` is a single-producer protocol, and Go offers no race-free way to make it safe once several producers exist |
| a choice of what happens when full | a channel suspends, and that is the only option |
| batched taking | draining a channel without racing the producer is awkward |

Everything else stays channel-shaped. Values come out in the order they went in,
and a shut-down queue drains before it reports that it is finished — the same
signal a closed channel gives, which is why `Take` returns the same
`Receive[A]`.

```go
func NewQueue[R, A any](capacity int, whenFull WhenFull) Effect[R, Never, Queue[A]]
func NewUnboundedQueue[R, A any]() Effect[R, Never, Queue[A]]
func (scope Scope) Queue[R, A any](capacity int, whenFull WhenFull) Effect[R, Never, Queue[A]]
func (scope Scope) UnboundedQueue[R, A any]() Effect[R, Never, Queue[A]]

func (queue Queue[A]) Offer[R any](value A) Effect[R, Never, bool]
func (queue Queue[A]) Take[R any]() Effect[R, Never, Receive[A]]
func (queue Queue[A]) TakeUpTo[R any](limit int) Effect[R, Never, []A]
func (queue Queue[A]) TakeAvailable[R any](limit int) Effect[R, Never, []A]
func (queue Queue[A]) Shutdown[R any]() Effect[R, Never, Unit]
func (queue Queue[A]) Size() int
func (queue Queue[A]) IsShutdown() bool
```

## When it is full

`WhenFull` is required rather than defaulted, because the wrong answer here is
the usual cause of a stalled or a lossy pipeline and a default would hide it.

| Choice | Behaviour |
|---|---|
| `SuspendWhenFull` | the offer waits for room, applying backpressure to the producer |
| `DropNewestWhenFull` | the incoming value is refused and the backlog kept |
| `DropOldestWhenFull` | the oldest queued value is discarded, keeping the most recent |

`Offer` reports whether the queue accepted the value, so a dropping queue's
losses are visible rather than silent. An unbounded queue never refuses, and its
memory is bounded only by its producers — a deliberate choice, not a
convenience.

## Shutdown

`Shutdown` is safe from any side and idempotent. It releases every parked taker
and offerer, refuses further offers, and **keeps what is already queued**, so a
consumer finishes the backlog before `Take` reports `OK == false`.

Prefer `Scope.Queue`. Work forked inside a scope is released by the cancellation
that closure performs first; the shutdown that follows is for anything still
holding the queue afterwards, which then learns the queue is finished instead of
blocking on it forever. Use the unscoped form only when the queue is meant to
outlive the scope that filled it.

## Batching

`TakeUpTo` waits for at least one value, so an empty result means the queue is
finished. `TakeAvailable` never waits, so an empty result means the queue was
empty at that instant and says nothing about whether it is finished.

Use `TakeUpTo` to consume a stream and `TakeAvailable` to drain a backlog.
Mixing them up is the difference between a consumer that terminates and one
that spins.

## Fairness

A value goes to the longest-waiting taker rather than to all of them, so a queue
with many consumers wakes one goroutine per item instead of every goroutine per
item. Item order is FIFO. A cancelled waiter that loses the race against a
handoff already in flight receives the value rather than discarding it, because
losing that race is not an error and dropping the value would be.

## Waiting

`Offer` on a full suspending queue and `Take` on an empty queue both wait, and
both are interruptible: cancellation yields an `Interrupt` cause carrying the
caller's reason, and the waiter removes itself.

## See also

- [Deferred](deferred.md), for a single value every waiter observes.
- [Hub](hub.md), for broadcasting to a changing set of subscribers.
