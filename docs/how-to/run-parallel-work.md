# How to run parallel work

## Two results

```go
both := effect.ZipPar(loadCustomer(id), loadBasket(id))
// Effect[R, E, Product[Customer, Basket]]
```

Different requirements or error types:

```go
merged := effect.ZipParChannels(loadCustomer(id), fetchQuote(id))
// Effect[Product[R1,R2], Either[E1,E2], Product[Customer, Quote]]
```

If one branch fails the other is canceled and awaited. Two independent failures
compose with `Both`, in positional order.

## A collection

```go
reports := effect.ForEachPar(paths, readSource)
all := effect.AllPar(effects)
```

Results are collected in input order however the branches interleave. The first
failure cancels the remaining branches, and the combinator still waits for all
of them.

## A bounded collection

```go
reports := effect.ForEachParN(paths, 4, readSource)
all := effect.AllParN(effects, 4)
```

At most four branches run at once. A branch waiting for a slot stays cancelable.
Bounding uses a buffered channel as a semaphore, so each branch is still one
ordinary goroutine owned by the private scope.

## First success

```go
fastest := effect.Race(primaryRegion(), secondaryRegion())
```

One branch failing does not end the race; the other keeps running and may still
succeed. If both fail, the cause is `Both` of theirs.

## First completion

```go
first := effect.RaceFirst(cachedValue(), freshValue())
```

Whatever finishes first wins, success or failure. This is the direct analogue of
selecting on two completion channels.

## Give up after a while

```go
guarded := load().TimeoutFail(2*time.Second, AppError{Reason: "slow source"})
optional := effect.Timeout(load(), 2*time.Second)   // Either[Unit, A]
fallback := load().TimeoutTo(2*time.Second, cached)
```

The wait uses the runtime clock, so a test drives it without sleeping. See
[test time](test-time.md).

## What every one of these guarantees

None returns while work it discarded is still executing or finalizing. That is
what makes a timeout safe to wrap around a transaction. A branch canceled only
because a sibling failed is not reported as an independent failure, but anything
else it reported on the way out is still composed in.

## Sequential when you want it

```go
effect.Zip(first, second)          // left then right
effect.ForEach(inputs, step)       // in order, short-circuiting
effect.All(effects)
```

## Working example

[`examples/parallelimport`](../../examples/parallelimport/program.go) reads
several sources with a bounded degree of parallelism under one scoped lock.
