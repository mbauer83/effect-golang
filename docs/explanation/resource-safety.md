# Resource safety

The guarantee is narrow enough to be true: **a resource that was successfully
acquired is released exactly once.**

## The race that matters

```text
goroutine A                    goroutine B

resource acquired
                              scope starts closing
register finalizer
```

If registration and closure are not coordinated, an acquired resource leaks.
Registration is therefore atomic with respect to scope state: while the scope is
open the finalizer is stored; once closure has begun the resource is released
immediately and the caller is told its lifetime has ended. There is no window in
which the resource exists and nobody owns it.

Registration also happens outside the interruption checkpoint. A cancellation
that arrives the instant after acquisition succeeds cannot skip it.

## Why release cannot fail with E

A single scope may hold a database handle, an open file and a listening server.
Their release errors have no common type, and those types cannot be added to an
effect's `E` after the effects have already been composed. Release therefore has
a `Never` failure channel.

It may still **defect**, and defects are preserved. A release error that matters
has three honest destinations and no fourth:

```go
// 1. Convert it to a defect.
effect.Release[R](func(ctx context.Context) error { return handle.Close() })
effect.OrDie(io.Remove(path))

// 2. Absorb it deliberately, having decided that is correct.
operations.LogWarn("could not remove lock").As(effect.Unit{})

// 3. Handle it before registration, so it can still participate in E.
```

Silently discarding it is not one of them.

## LIFO, and why

Resources depend on resources acquired before them:

```text
database -> transaction -> statement
```

so destruction reverses acquisition. Children terminate before any of it,
because a child may still be using the database.

## Cleanup survives cancellation

Finalizers run with `context.WithoutCancel`. Using the caller's canceled context
would make release abort immediately, which is the opposite of the guarantee.
That context keeps its values and drops its deadline, so a finalizer that
ignores cancellation can block indefinitely. The core deliberately favours
releasing the resource over truncating cleanup.

## Failures compose, they do not compete

```text
main effect  -> Fail(E)
finalizer #2 -> Die(D2)
finalizer #1 -> Die(D1)

Then(Fail(E), Then(Die(D2), Die(D1)))
```

A cleanup defect turns a successful body into a failure and never replaces the
body's own failure.

## Retry and placement

`Retry` re-evaluates in the current dynamic scope and never invents a hidden one,
because the value it returns may refer to a resource the caller owns. That makes
`Scoped` placement meaningful, and the
[retry reference](../reference/retry.md) states both forms.
