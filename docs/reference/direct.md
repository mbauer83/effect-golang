# Direct style reference

`experimental/direct` is an alternative to [`Workflow`](core.md) for a dependent
sequence. It is experimental, and the reasons are in
[sequencing in Go](../explanation/sequencing-in-go.md).

```go
func Run[R, E, A any](body func(*Binder[R, E]) A) effect.Effect[R, E, A]
func Bind[R, E, A any](binder *Binder[R, E], fx effect.Effect[R, E, A]) A
```

```go
program := direct.Run(func(bind *direct.Binder[Env, AppError]) Quote {
    customer := direct.Bind(bind, loadCustomer(id))
    basket := direct.Bind(bind, loadBasket(customer))
    return price(customer, basket)
})
```

## What behaves exactly as the core operators do

Everything except the short-circuit, because it is built on the public API and
not on a second mechanism.

- The effect is a description. `Run` evaluates nothing; the body runs when the
  effect is interpreted, once per interpretation.
- One `Run` value is reusable. Its per-run state is created per run.
- Interruption is observed. A bound effect is a normal interpretation, so it
  checks cancellation where any effect would.
- A bound effect sees the surrounding interpretation: the runtime's clock,
  filesystem, logger and observer, the enclosing scope, and the current
  observation metadata. A resource acquired through a bound `AcquireRelease` is
  released by the scope that owns it.
- A panic in the body becomes a defect, exactly as it would inside `From`.

## What Bind does with each outcome

| Outcome | Effect |
|---|---|
| success | returns the value; the body continues |
| typed failure | abandons the body; the failure becomes the effect's failure |
| defect | abandons the body; the defect stays a defect |
| interruption | abandons the body; the interruption stays an interruption |

A composite cause is passed through whole. `Bind` never selects one failure out
of a `Both`, and never discards a cleanup defect from a `Then`.

## How the short-circuit works

`Bind` must return an `A` while also abandoning the body when the effect did not
succeed. Go has no resumable suspension and no typed early-return protocol, so
the implementation panics with a private sentinel.

The sentinel's type is unexported, and each `Run` carries its own token, so a
nested `Run` recovers only its own short-circuit. An inner `Run`'s failure is an
ordinary typed failure to the outer body, which can therefore handle it with
`CatchAll` like any other.

## The two hazards

**A `defer` in the body runs on every expected failure.** Not only on a panic.
A body whose `defer` assumes it is cleaning up after an exception will run that
cleanup on an ordinary domain failure.

**A broad `recover()` in the body can swallow the short-circuit.** This cannot
be prevented. It is detected: if the body returns a value although a `Bind` had
short-circuited, the sentinel was swallowed, and the result is a defect naming
the cause that was abandoned. Reporting that is better than returning a result
the program never computed, but the detection is after the fact.

## Misuse that is reported rather than silent

A `Binder` used after its `Run` body returned reports a defect naming the
mistake, instead of evaluating against an interpretation that has ended. A
`Binder` must not be published: hold the `Effect` values and interpret them
inside your own `Run`.

## The seam it is built on

```go
func Interpreting[R, E, A any](body func(Interpreter[R, E]) Exit[E, A]) Effect[R, E, A]
func Evaluate[R, E, A any](interpreter Interpreter[R, E], fx Effect[R, E, A]) Exit[E, A]
func (interpreter Interpreter[R, E]) Context() context.Context
```

`Interpreting` is public and useful beyond direct style: it is how any caller
writes a combinator this package does not provide. `Evaluate` interprets an
effect inside the *current* interpretation, which is the point — reaching for
the package-level `Run` instead would silently give the effect a fresh runtime
with live defaults, a scope of its own and no cancellation, so a `Sleep` would
ignore a test clock and an acquisition would register in a lifetime nobody
closes.

An `Interpreter` is valid only while the body that received it is running.

## Cost

Measured, not assumed. The numbers and what they do and do not justify are in
[sequencing in Go](../explanation/sequencing-in-go.md#what-it-measures). The
short version: direct style is cheaper than `Workflow` on the success path and
about a quarter more expensive on the failure path, so **cost is not a reason to
prefer `Workflow`** — the two hazards above are.

## Choosing

Prefer `Workflow`. Reach for direct style when the explicit state type is the
thing making a workflow hard to read, and when the body contains no `defer` you
would be surprised to see run on a failure.

[`examples/checkout`](../../examples/checkout/program.go) is written both ways,
and an end-to-end test asserts the two agree on every path.
