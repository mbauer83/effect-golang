# Fiber reference

`Fiber[E,A]` observes one forked computation. It is a handle: copies observe the
same computation.

A fiber stores its terminal `Exit` and then closes a `done` channel. Closing a
channel is a broadcast that happens before any receive observing it, so the
result needs no lock and every observation is repeatable by any number of
concurrent observers. A result channel would instead be consumed by its first
receiver.

## Creation

```go
func Fork[R, E, A any](fx Effect[R, E, A]) Effect[R, Never, Fiber[E, A]]
func (scope Scope) Fork[R, E, A any](fx Effect[R, E, A]) Effect[R, Never, Fiber[E, A]]
func ForkDaemon[R, E, A any](fx Effect[R, E, A]) Effect[R, Never, Fiber[E, A]]
```

A child receives its own cancelable context and its own scope, so its resources
and descendants are released before it completes and cannot outlive it.

Because the failure channel is `Never`, composing a fork with failing work needs
one widening. `Operations[R,E]` provides the pre-widened forms:
`operations.Fork`, `operations.ForkIn`, `operations.ForkDaemon`.

## Observation

```go
func (fiber Fiber[E, A]) Await[R any]() Effect[R, Never, Exit[E, A]]
func (fiber Fiber[E, A]) Join[R any]()  Effect[R, E, A]
func (fiber Fiber[E, A]) Interrupt[R any]() Effect[R, Never, Exit[E, A]]
func (fiber Fiber[E, A]) Poll() (Exit[E, A], bool)
func (fiber Fiber[E, A]) Done() <-chan struct{}
func (fiber Fiber[E, A]) ID() uint64
```

`Await` yields the complete outcome without adopting it; waiting is itself
interruptible. `Join` adopts the outcome: a typed child failure becomes a typed
caller failure, a defect stays a defect, an interruption stays an interruption.

`Interrupt` requests cancellation and returns only once the fiber has
terminated, its child scopes have closed and its finalizers have run. It is
deliberately not fire-and-forget; returning early would leave cleanup
unobserved. It does not select on the caller's context for the same reason.

`Poll` reports the result when the fiber has already completed. `Done` exposes
completion for an ordinary `select`; it is a synchronization signal and never
yields the result.

The requirement channel is an explicit type argument because a fiber captured
its own `R` when it was forked. `Operations[R,E]` supplies it:
`operations.Await`, `operations.Join`, `operations.Interrupt`.

## Ownership

Scope closure cancels every fiber the scope owns and waits for all of them
before releasing any resource. A fiber therefore cannot outlive the resources it
may be using, and no fiber the effect API creates is without a lifetime owner.

`ForkDaemon` work outlives the `Run` that created it and is bounded by
`Runtime.Close`.
