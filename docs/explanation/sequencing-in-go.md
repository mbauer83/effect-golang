# Sequencing in Go, and the limits of sugar

Long `FlatMap` chains are a real usability problem, especially when a later step
depends on several earlier values. It is worth being precise about why Go cannot
simply borrow the usual answers.

## What the other runtimes rely on

ZIO's `for` comprehension is language-level desugaring. Effect's `yield*` needs
resumable generators. A Go library can add neither.

Go's range-over-function iterators are not an equivalent mechanism. Their
`yield` callback sends values *from* the iterator *to* the loop; the loop cannot
resume the producer with an arbitrarily typed result. They also require one
homogeneous iteration type. Encoding heterogeneous effect results through `any`,
reflection and hidden goroutines would lose the static channels and complicate
cancellation, so it is not used as fake do-notation.

## First remove the dependency

Most long chains are not actually dependent. Before reaching for syntax:

- `Map`, `Zip` and `ZipPar` for independent work;
- `All`, `ForEach` and their parallel variants for collections;
- `As`, `Tap`, `Flatten` and `AndThen` for common shapes;
- small named functions for meaningful workflow stages.

Go does not infer generic arguments from an expected result type, so a
zero-argument primitive such as `Now[R, E]()` cannot infer either phantom
channel. Rather than repeating those arguments throughout a program, select them
once:

```go
operations := effect.For[Env, AppError]()
now := operations.Now()

io := effect.IO() // IOOperations[Unit], error channel IOError
loaded := effect.Zip(io.ReadFile(path), io.Now())
```

`Operations[R,E]` holds no runtime state. It carries compile-time channel
evidence into ordinary constructors, which is why it can also supply the
pre-widened fiber and channel operations whose own failure channel is `Never`.

## Then flatten the layout

`Workflow` packages successive `FlatMap` steps around one caller-declared state
type. It is the closest Go analogue to Effect's non-generator `Do`/`bind`:
TypeScript can grow a structural record after every bind, Go cannot, so one
explicit state type carries the whole workflow.

It is a convenience over the core algebra, not a runtime mode. Failures,
defects, cancellation and stack-safety behave identically. The state factory
runs once per interpretation, so a retry or a concurrent run never inherits
another run's partial state. See the
[checkout example](../../examples/checkout/program.go).

It trades indentation for an explicit state struct. That is a good trade for a
workflow with several cross-step dependencies and a poor one for a two-step
composition.

## Direct style, and what it actually costs

An API shaped like `user := direct.Bind(bind, LoadUser())` is attractive, and it
exists in `experimental/direct`. `Bind` must return an `A` while also abandoning
the callback when the effect did not succeed, and Go has no resumable suspension
and no typed early-return protocol, so the implementation uses a private panic
sentinel carrying a token unique to one `Run`.

Two of the risks turn out to be detectable rather than merely documentable:

- a `Binder` used after its body returned reports that, instead of evaluating
  against an interpretation that has ended;
- a `recover()` in the body that swallows the sentinel is caught afterwards — if
  the body returns a value although a `Bind` short-circuited, the sentinel was
  swallowed, and reporting a defect is better than returning a result the
  program never computed.

Two costs remain and cannot be fixed. A `defer` block in the body runs on every
expected failure, not only on an exceptional one; and a panic is paid for on the
expected-failure path.

### What it measures

Three-step dependent workflow, same steps, three styles
(`test/benchmark`, Go 1.27.1, i7-13700H):

| Style | Success | Failure |
|---|---|---|
| `FlatMap` | 438 ns, 10 allocs | 470 ns, 9 allocs |
| `Workflow` | 802 ns, 24 allocs | 685 ns, 18 allocs |
| `direct` | 609 ns, 11 allocs | 861 ns, 10 allocs |

The panic costs about 250 ns, which is the gap between direct style's own
success and failure paths.

This is not the result the design expected, and it is worth stating plainly:
**direct style is cheaper than the safe builder on the success path**, in time
and in allocations, because `Workflow` allocates a closure pair and copies its
state on every `Bind`. On the failure path it is about 26% more expensive than
`Workflow`.

The steps here are `Succeed`, so these numbers are almost all framework
overhead. Against any real work — a query, a file, a request — all three
collapse into noise.

### What that changes, and what it does not

The recommendation is still to prefer `Workflow`, but **the cost argument does
not support that preference and should not be used to justify it.** The reasons
are the two unfixable ones above: a `defer` that runs on an expected path, and a
`recover()` that can swallow a short-circuit. Those are properties of the
approach, not of its speed.

Reach for direct style when the explicit state type is the thing making a
workflow hard to read, and when the body contains no `defer` you would be
surprised to see run on a failure.

## Why not a source generator

A generator could invent do-syntax and emit `FlatMap`, but it would add a second
source language, generated-code navigation problems and another compatibility
surface, and Go has no hygienic macro facility to make it transparent.

Generation is useful where the output is a boring adapter a person could have
written: an application façade that fixes `R` and `E` once, a declared error
mapping, a stub for a narrow port. It must not infer mappings by naming
convention, rewrite user function bodies, or generate a hidden control-flow
protocol. `go generate` is not run by `go build`, so hand-written `Operations`
remains the dependable base API, and the library and its tutorials stay ordinary
Go with no generation step.
