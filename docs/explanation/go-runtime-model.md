# The Go runtime model, and where this library differs

This library does not implement ZIO's or Effect's execution model. It implements
their useful *guarantees* on top of Go's own runtime.

## The mapping

```text
effect fiber    -> goroutine
suspension      -> goroutine parked by Go
completion      -> closed channel
interruption    -> context cancellation
waiting/racing  -> select
structured life -> Scope
```

The boundary is worth stating plainly:

> The effect runtime owns **semantics and lifetimes**, not CPU scheduling.

ZIO needs a fiber scheduler because it implements lightweight fibers over the
JVM. Go already has goroutines and a scheduler. A second scheduler would compete
with it for no gain, so `Sleep`, `Await`, `Join`, channel operations and races
simply block or `select` in the ordinary Go manner.

## Where the divergences are deliberate

**Cancellation is cooperative.** We cannot truthfully promise interruption of
arbitrary Go code. An effect that never observes its context cannot be stopped.
The runtime guarantees its own combinators cooperate and documents the rest; see
[interruption](../reference/interruption.md).

**Acquisition is not made uninterruptible.** ZIO strips cancellation from
acquisition. In Go that would fight `db.QueryContext`, `DialContext` and
`http.NewRequestWithContext`, and could turn cancellation into an indefinite
wait. The guarantee offered instead is narrower and honest: a resource that was
successfully acquired is owned before the runtime reaches another interruption
checkpoint. Cleanup of partially created state belongs to the acquisition, which
is already the conventional contract of a context-aware Go API.

**Channel closure is not an error.** Go's `value, ok := <-ch` is preserved as a
value. A send to a closed channel is a defect, because that is exactly what
Go's panic already means. See [channels](../reference/channels.md).

**Scope is lexical, not part of `R`.** ZIO writes `ZIO[R & Scope, E, A]` and
subtracts `Scope`. A structural `Product` environment cannot subtract, so a
scope in `R` would leak into every resource-using type. `Scoped` takes it as a
parameter instead.

**`errgroup` is not the core abstraction.** It propagates one ordinary `error`.
This runtime needs a typed `E`, a complete `Cause` tree, repeatable fiber
observation, scopes and explicit interruption semantics. It remains excellent
prior art for bounded concurrency.

## What Go gives us for free

`sync.WaitGroup.Go` tracks spawned work, including work that spawns more work
while the group is non-empty, which is what a scope needs. `context.WithCancelCause`
distinguishes *why* work was canceled from generic cancellation, which is what
makes a useful `Interruption`. Closing a channel is a broadcast that happens
before any receive observing it, which is what makes `Await` repeatable without
a lock.

Low-level lifecycle bookkeeping uses a mutex. Go recommends channels for
higher-level coordination, not for contorting state machines.
