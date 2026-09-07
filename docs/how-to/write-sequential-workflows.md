# How to write sequential workflows

## First, remove the dependency

Most long chains are not really dependent. `FlatMap` is only needed when a later
step needs an earlier *value*.

```go
effect.Zip(io.ReadFile(path), io.Now())          // independent, sequential
effect.ZipPar(loadCustomer(id), loadBasket(id))  // independent, concurrent
effect.ForEach(paths, readSource)                // a collection, in order
loaded.As(report)                                // discard the value
loaded.Tap(logSize)                              // inspect and keep it
loaded.AndThen(write)                            // sequence and take the second
effect.Flatten(nested)                           // remove one layer
```

## Select the channels once

Go cannot infer a generic argument from an expected result type, so
`Now[R, E]()` cannot infer either phantom channel. Declare them once:

```go
operations := effect.For[Env, AppError]()
now := operations.Now()
log := operations.LogInfo("starting")

io := effect.IO()                       // IOOperations[Unit], errors are IOError
loaded := effect.Zip(io.ReadFile(path), io.Now())
```

`Operations` holds no runtime state; it carries compile-time channel evidence.
It also supplies the pre-widened fork, fiber and channel operations whose bare
forms have a `Never` failure channel.

In dense application code an import alias such as `fx` is fine. Dot imports are
not recommended: losing symbol provenance is a poor trade for a short qualifier.

## Then flatten the layout

When several later steps genuinely depend on several earlier values, an explicit
state type keeps the source flat:

```go
type state struct {
    customer Customer
    basket   Basket
    quote    Quote
}

program := operations.Do(func() state { return state{} }).
    Bind(
        func(state) workflowEffect[Customer] { return loadCustomer(id) },
        func(current state, customer Customer) state {
            current.customer = customer
            return current
        },
    ).
    Bind(
        func(current state) workflowEffect[Basket] { return loadBasket(current.customer) },
        func(current state, basket Basket) state {
            current.basket = basket
            return current
        },
    ).
    Yield(func(current state) Quote { return current.quote })
```

`Bind` is typed `FlatMap` plus a state transition, so failures, defects,
cancellation and stack safety are identical to the core operators. The state
factory runs once **per interpretation**, so a retry or a concurrent run never
inherits another run's partial state.

Return a new state value rather than mutating shared data. A state value can
still hold pointers, slices or maps; the builder is sequential and does not
synchronize what those refer to.

## Know when not to use it

The builder trades indentation for an explicit state struct and transitions.
That is a good trade for a workflow with several cross-step dependencies and a
poor one for a two-step composition — use `FlatMap` or a small named function
there.

## Naming stages helps more than you expect

```go
loadCustomer(id).Named("load-customer")
program.WithSpan("checkout", slog.String("customer", id))
```

Names and annotations flow into log records and lifecycle events, and a span
records its call site once when it is described.

## The concrete shape, from a real program

```go
operations := effect.IO()
loaded := effect.Zip(
    operations.ReadFile(inputPath),
    operations.Now(),
).Map(func(values effect.Product[[]byte, time.Time]) sourceSnapshot {
    return sourceSnapshot{content: values.First, observedAt: values.Second}
})

program := loaded.FlatMap(normalizeAndStore(operations, inputPath, outputPath))
```

This is preferable to emulating do-notation for a short workflow: it is ordinary
Go, has no exceptional control flow, and preserves `R`, `E` and `A` exactly.

Go range functions cannot implement an Effect-TS style `yield*`. A range
iterator yields homogeneous values outward and cannot resume with an arbitrarily
typed result, so panic-based direct style is not part of the core API. The
reasoning is in
[sequencing in Go](../explanation/sequencing-in-go.md).

## Use a short package alias in dense programs

Go requires imported identifiers to stay qualified. An ordinary import alias
shortens effect-heavy code without hiding ownership:

```go
import fx "github.com/mbauer83/effect-golang/effect"

operations := fx.IO()
program := fx.Zip(read, operations.Now())
```

`fx` is clearer than a one-letter alias in application code. Avoid dot imports:
unqualified exported names collide easily and make provenance harder to see in
reviews, search results and documentation. Foundational documentation keeps the
explicit `effect` name.

## Working examples

- [`examples/checkout`](../../examples/checkout/program.go) uses the state
  builder for a three-step dependent workflow.
- [`examples/filecopy`](../../examples/filecopy/program.go) uses `Zip` and a
  named stage instead of nested `FlatMap`s.
