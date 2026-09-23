# Direct style reference

`experimental/direct` writes a dependent sequence of effects as ordinary Go. It
is the recommended way to write one; the reasons, and what it replaced, are in
[sequencing in Go](../explanation/sequencing-in-go.md).

```go
func Run[R, E, A any](body func(*Do[R, E]) A) effect.Effect[R, E, A]
func (do *Do[R, E]) Await[A any](fx effect.Effect[R, E, A]) A
func (do *Do[R, E]) Fail(failure E)
```

```go
program := direct.Run(func(do *direct.Do[Env, AppError]) Quote {
    customer := do.Await(loadCustomer(id))
    basket := do.Await(loadBasket(customer))
    if basket.IsEmpty() {
        do.Fail(ErrEmptyBasket)
    }
    return price(customer, basket)
})
```

## What behaves exactly as the core operators do

Everything, because every awaited effect is interpreted by the surrounding
interpretation and not by a second mechanism.

- The effect is a description. `Run` evaluates nothing; the body runs when the
  effect is interpreted, once per interpretation.
- One `Run` value is reusable. A retry runs the body again, and concurrent runs
  each have their own body and their own `Do`.
- Interruption is observed at the next `Await`, where any effect would observe
  it.
- An awaited effect sees the surrounding interpretation: the runtime's clock,
  filesystem, logger and observer, the enclosing scope, and the current
  observation metadata. A resource acquired through an awaited `AcquireRelease`
  is released by the scope that owns it.
- A panic in the body becomes a defect, exactly as it would inside `From`.

## What Await does with each outcome

| Outcome | Effect |
|---|---|
| success | returns the value; the body continues |
| typed failure | ends the body; the failure becomes the effect's failure |
| defect | ends the body; the defect stays a defect |
| interruption | ends the body; the interruption stays an interruption |

A composite cause is passed through whole. `Await` never selects one failure out
of a `Both`, and never discards a cleanup defect from a `Then`. `Fail` is
`Await` of a failed effect, written where the judgement is made.

## How a body ends early

`Await` must return an `A` and must also abandon the body when the effect did not
succeed, and Go has no resumable suspension. So the body runs on a goroutine of
its own, in lock step with the interpretation: the interpretation waits while
the body runs, and the body evaluates what it awaits with the interpretation's
context, scope and capabilities. A failed `Await` ends the body's goroutine with
`runtime.Goexit`.

Two properties follow, and they are why this replaced a panic:

- **Nothing can swallow a failure.** `recover()` does not see a `Goexit`, so a
  broad `recover()` in the body finds nothing and the failure reaches the effect
  as it was.
- **A `defer` is a finalizer.** A `Goexit` runs the body's deferred calls, so a
  `defer` runs on a failure as on a success — what a `finally` block is in
  Effect's `gen` and `ensuring` is in ZIO.

An inner `Run`'s failure is an ordinary typed failure to the outer body, which
can handle it with `CatchAll` like any other.

## Misuse that is reported rather than silent

- A `Do` used after its body ended reports a defect naming the mistake, instead
  of evaluating against an interpretation that has ended. Hold the `Effect`
  values and interpret them inside your own `Run`.
- A `Do` used from a goroutine the body started reports a defect when the two
  overlap. `Await` belongs to the body's own goroutine; fork an effect instead.
- A `runtime.Goexit` of the body's own — `testing.T.FailNow` inside a body is
  the realistic case — ends the body and is reported as a defect.

## Where it stops

**Recursion through `Run`.** Every running body holds a goroutine, and a body
awaiting another body holds one for each. A handler awaiting a service awaiting
a repository is three; a program that recurses through `Run` a million deep is a
million, where the same recursion through `FlatMap` is stack-safe. Loop with
`for` inside one body.

**What belongs to a goroutine.** The body is not the goroutine that called
`Run`, so `runtime.LockOSThread` and profiler labels do not carry over.

**Recovery part-way through.** `Await` ends the body, so there is no way to
catch a failure and carry on inside one. Await the effect with `CatchAll` or
`OrElse` applied, and the body continues with whatever that answers.

## The seam it is built on

```go
func WithInterpreter[R, E, A any](body func(Interpreter[R, E]) Exit[E, A]) Effect[R, E, A]
func Interpret[R, E, A any](interpreter Interpreter[R, E], fx Effect[R, E, A]) Exit[E, A]
func (interpreter Interpreter[R, E]) Context() context.Context
```

`WithInterpreter` is public and useful beyond direct style: it is how any caller
writes a combinator this package does not provide. `Interpret` interprets an
effect inside the *current* interpretation, which is the point — reaching for
the package-level `Run` instead would silently give the effect a fresh runtime
with live defaults, a scope of its own and no cancellation, so a `Sleep` would
ignore a test clock and an acquisition would register in a lifetime nobody
closes.

An `Interpreter` is valid only while the body that received it is running.

## Cost

A run costs about one goroutine hand-off on top of what `FlatMap` costs, however
many effects it awaits; a failed run costs a fresh goroutine, a few microseconds
more. The numbers are in
[sequencing in Go](../explanation/sequencing-in-go.md#what-it-measures). Against
any real work — a query, a file, a request — that is noise. For an effect run per
element of a hot stream, build with [`effectgo`](../how-to/rewrite-direct-style.md),
which rewrites the bodies it can translate into `FlatMap` chains, or write
`FlatMap`.

## Choosing

**Use direct style for a dependent sequence.** A sequence of three or four steps
written as `FlatMap`s nests each step inside the one before it, so the last
thing to happen is indented deepest and the reading order is the reverse of the
doing order.

`FlatMap` is still the shorter form for a single step passed point-free —
`open(…).FlatMap(await)` — and for a `Map` over one value.

A refusal is a guard clause:

```go
direct.Run(func(do *direct.Do[Env, Refusal]) Book {
    held := do.Await(store.All())
    index := slices.IndexFunc(held, sameTitle(title))
    if index < 0 {
        do.Fail(Refusal{Kind: NotFound})
    }
    return held[index]
})
```

[`examples/checkout`](../../examples/checkout/program.go) is written both ways,
and an end-to-end test asserts the two agree on every path.
