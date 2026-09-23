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

## Or drop the state type, experimentally

`experimental/direct` writes the same workflow without a state type, because
each `Bind` returns the value the next line uses:

```go
program := direct.Run(func(bind *direct.Binder[Env, AppError]) Quote {
    customer := direct.Bind(bind, loadCustomer(id))
    basket := direct.Bind(bind, loadBasket(customer))
    return price(customer, basket)
})
```

A failing `Bind` abandons the rest of the body, and the failure, defect or
interruption reaches the effect unchanged. Everything else behaves as the core
operators do: the effect stays lazy, one value is reusable, cancellation is
observed, and a bound effect sees the surrounding runtime and scope.

Two things to know before choosing it:

- **A `defer` in the body runs on every expected failure**, not only on a panic.
  Write the body without one, or use `Workflow`.
- **A broad `recover()` in the body can swallow the short-circuit.** That is
  detected and reported as a defect rather than returning a value the program
  never computed, but it is detected after the fact.

Cost is *not* a reason to prefer `Workflow`: direct style is
[measurably cheaper on the success path](../explanation/sequencing-in-go.md#what-it-measures).
The two hazards above are the reason.

[`examples/checkout`](../../examples/checkout/program.go) is written both ways,
and an end-to-end test asserts they agree on every path. The exact semantics are
in the [direct style reference](../reference/direct.md).

## Know when not to use it

The builder trades indentation for an explicit state struct and transitions.
That is a good trade for a workflow with several cross-step dependencies and a
poor one for a two-step composition — use `FlatMap` or a small named function
there.

## Naming stages helps more than you expect

```go
loadCustomer(id).WithName("load-customer")
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
