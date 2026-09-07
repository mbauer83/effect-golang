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

## Why not direct style

An API shaped like `user := d.Bind(LoadUser())` is superficially attractive, but
`Bind` must return an `A` while also abandoning the callback on failure. Go has
no resumable suspension and no typed early-return protocol, so an implementation
needs a private panic sentinel. That is containable, and Go 1.27 generic methods
can express it, but it costs:

- user `defer` blocks running on every expected failure;
- a broad `recover` in user code intercepting the sentinel;
- less intuitive stack traces;
- panic cost on an expected path.

Direct style is therefore not a foundational API and not an internal mechanism.
If demand survives real use of the safe builder, it belongs in a clearly
experimental subpackage.

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
