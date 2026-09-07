# Concurrency

This tutorial shows the happy path. Fork some work, wait for it, and run two
things at once.

## Fork and join

`Fork` starts an effect on its own goroutine and gives you a `Fiber` to observe.
Because forking cannot fail, its failure channel is `Never`; selecting your
program's channels once with `For` gives you a form that composes directly.

```go
operations := effect.For[effect.Unit, ImportError]()

program := effect.Scoped(func(scope effect.Scope) effect.Effect[effect.Unit, ImportError, Report] {
    return operations.Fork(loadRecords()).
        FlatMap(operations.Join)
})
```

`Scoped` is not decoration. It is the lifetime that owns the child: when the
scope closes, the child is canceled and awaited.

## Two things at once

When both results are wanted, use `ZipPar`:

```go
both := effect.ZipPar(loadCustomer(id), loadBasket(id))
// Effect[R, E, Product[Customer, Basket]]
```

Both branches run concurrently. If one fails, the other is canceled and awaited
before `ZipPar` completes.

## First one wins

```go
fastest := effect.Race(primaryRegion(), secondaryRegion())
```

`Race` waits for the first **success**, so one region failing does not end the
race. If you want the first branch to *finish* to win, whatever its outcome,
that is `RaceFirst`.

## Many things at once

```go
reports := effect.ForEachParN(paths, 4, readSource)
// Effect[R, E, []Report], at most four readers at a time
```

Results come back in input order however the branches interleave.

## Giving up after a while

```go
guarded := loadRecords().TimeoutFail(2*time.Second, ImportError{Reason: "slow source"})
```

The wait uses the runtime clock, so a test can drive it without sleeping. The
timed-out work is canceled *and awaited*, including its finalizers, before
`TimeoutFail` returns.

## Working example

[`examples/parallelimport`](../../examples/parallelimport/program.go) imports
several files concurrently under one scoped lock, with a bounded number of
readers. Its end-to-end test asserts the results, the released lock and the
recorded lifecycle events.

## Next

- [Fork and join](../how-to/fork-and-join.md)
- [Run parallel work](../how-to/run-parallel-work.md)
- [Cancel work](../how-to/cancel-work.md)
- [Structured concurrency](../explanation/structured-concurrency.md)
