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

## Then write it in direct style

When several later steps genuinely depend on several earlier values, write the
sequence as ordinary Go. `experimental/direct` does that, because each `Await`
returns the value the next line uses:

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

A failing `Await` ends the rest of the body, and the failure, defect or
interruption reaches the effect unchanged. A `defer` in the body runs on that
failure as on a success, and a `recover()` cannot swallow it. Everything else
behaves as the core operators do: the effect stays lazy, the body runs afresh
for each interpretation so a retry or a concurrent run never inherits another
run's partial state, cancellation is observed, and an awaited effect sees the
surrounding runtime and scope.

Loop with `for` inside one body rather than recursing through `Run`: each
running body holds a goroutine.

[`examples/checkout`](../../examples/checkout/program.go) is written this way
and once more as the `FlatMap` chain it describes, and an end-to-end test
asserts the two agree on every path. The exact semantics are in the
[direct style reference](../reference/direct.md).

## Know when not to use it

A single step reads better as `FlatMap`, and a `Map` over one value better
still. And for an effect run per element of a hot stream, the goroutine a body
runs on is a cost worth removing: build with
[`effectgo`](rewrite-direct-style.md), which rewrites the bodies it can into
`FlatMap` chains, or write the chain.

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

- [`examples/checkout`](../../examples/checkout/program.go) writes a
  three-step dependent workflow in direct style, and as a `FlatMap` chain.
- [`examples/filecopy`](../../examples/filecopy/program.go) uses `Zip` and a
  named stage instead of nested `FlatMap`s.
