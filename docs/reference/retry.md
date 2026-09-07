# Retry reference

Retry is ordinary sequential effect evaluation plus interruptible clock waits.
Each attempt replaces the previous one in the interpreter's instruction stream
rather than nesting inside it, so an unbounded policy adds no stack frames.

## Operations

```go
func (fx Effect[R, E, A]) Retry[Out any](policy Schedule[E, Out]) Effect[R, E, A]
func (fx Effect[R, E, A]) RetryCause[Out any](policy Schedule[Cause[E], Out]) Effect[R, E, A]
func (fx Effect[R, E, A]) RetryN(count uint64) Effect[R, E, A]
func (fx Effect[R, E, A]) RetryOrElse[Out any](policy Schedule[E, Out], fallback func(E, Out) Effect[R, E, A]) Effect[R, E, A]
func (fx Effect[R, E, A]) Repeat[Out any](policy Schedule[A, Out]) Effect[R, E, Out]
func (fx Effect[R, E, A]) Delay(duration time.Duration) Effect[R, E, A]
```

## Eligibility

```text
evaluate fx immediately
  success           -> return A
  Die               -> return Die; never retried
  Interrupt         -> return Interrupt; never retried
  Fail(E)           -> feed E to a fresh driver
      done          -> return the last Fail(E)
      continue(d)   -> wait interruptibly, then evaluate again
```

`Retry` accepts only an exact `Fail` leaf. It does not choose one failure from
`Both(Fail(a), Fail(b))` and does not discard a cleanup defect from
`Then(Fail(e), Die(d))`.

`RetryCause` opts in to a composite cause whose every leaf is a typed failure.
It still rejects any tree containing a defect or an interruption.

Retrying a defect can repeat corrupting logic; retrying an interruption would
violate structured cancellation.

Interruption is checked before the policy is consulted and before another
attempt begins. A typed failure reported after the context was already canceled
loses to the observed interruption.

## Exhaustion

`Retry` returns the **last typed failure**, which keeps its error channel
stable. `RetryOrElse` receives that failure and the policy's final output.

`RetryN(3)` means at most three retries after the initial attempt: four
evaluations.

## Repetition

`Repeat` evaluates once, feeds each successful value to a fresh driver, and runs
again only when the decision continues. Any typed failure, defect or
interruption stops repetition. Its result is the schedule's final output.

Repetition is sequential: one run finishes before the delay and the next run. It
never overlaps runs. Use `Fork` with a scope to make concurrency explicit.

## Resources across attempts

`Retry` re-evaluates in the current dynamic scope. It does not invent a hidden
scope, because the returned value may refer to a resource the caller's scope
owns. Placement is therefore significant:

```go
// Each attempt owns and releases its resources before Retry sees its Exit.
Scoped(useResource).Retry(policy)

// All attempts share the outer scope; failed attempts keep their registrations
// until it closes.
Scoped(func(scope Scope) Effect[R, E, A] { return useResource.Retry(policy) })
```

The first form is the default for retrying an acquire-use workflow. A finalizer
is never re-run by retry: it runs according to its owning scope, exactly once per
successful registration.

## Observation

Retry emits `retry_scheduled`, `retry_exhausted` and `retry_succeeded` events
carrying the attempt number, the selected delay and a bounded status. Repetition
emits `repeat_scheduled` and `repeat_completed`. No failure value or environment
is attached.
