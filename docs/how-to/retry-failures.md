# How to retry failures

## Retry a typed failure

```go
loaded := readSource(path).Retry(effect.Recurs[effect.IOError](3))
```

`RetryN(3)` is the same thing: at most three retries after the initial attempt,
four evaluations in total. The first attempt is always immediate.

## Back off

```go
policy := effect.IntersectSchedules(
    effect.Recurs[effect.IOError](5),
    effect.Exponential[effect.IOError](100*time.Millisecond, 5*time.Second),
)

loaded := readSource(path).Retry(policy)
```

`IntersectSchedules` continues only while both continue and waits the longer delay, so
this is "exponential backoff, at most five retries".

## Add jitter

```go
policy = policy.Jittered(rand.Float64, 0.5, 1.5)
```

Randomness is injected, not taken from package-global state, so a test can pass
a deterministic function.

## Keep the last failure, or substitute something

Exhaustion returns the **last typed failure**, which keeps the error channel
stable. When you want the policy's output or a fallback:

```go
readSource(path).RetryOrElse(policy, func(last effect.IOError, attempts uint64) ioEffect {
    return operations.LogWarn("falling back to cache").AndThen(readCache())
})
```

## What is never retried

Defects and interruption. Retrying a defect can repeat corrupting logic, and
retrying an interruption would violate structured cancellation.

A composite cause is not retried either: `Retry` will not pick one failure out
of `Both(Fail(a), Fail(b))` and will not discard the cleanup defect in
`Then(Fail(e), Die(d))`. To retry a composite whose every leaf is a typed
failure, opt in:

```go
fx.RetryCause(effect.Recurs[effect.Cause[AppError]](3))
```

That still rejects any tree containing a defect or an interruption.

## Retry an acquire-use workflow

```go
attempt := effect.Scoped(func(scope effect.Scope) effect.Effect[Env, AppError, Result] {
    return scope.AcquireRelease(acquire(), release).FlatMap(use)
})

program := attempt.Retry(policy)
```

`Scoped` inside means each attempt releases its own resources before `Retry`
observes the outcome. `Scoped` outside means all attempts share one lifetime and
failed attempts keep their registrations until it closes. Both are valid; the
first is the default.

## Repeat a success

```go
polled := checkStatus().Repeat(effect.Spaced[Status](time.Second).WhileInput(isPending))
```

`Repeat` runs once immediately, then recurs on successful values. Any failure,
defect or interruption stops it. Runs never overlap; use `Fork` with a scope if
you want that.

## Watch it happen

Retry emits `retry_scheduled`, `retry_exhausted` and `retry_succeeded` events
carrying the attempt number and the chosen delay. Install a `RecordingObserver`
in a test, or your own for production.

## Working example

[`examples/filecopy`](../../examples/filecopy/program.go) retries a transient
source read with a caller-supplied policy.
