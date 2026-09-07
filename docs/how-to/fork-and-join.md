# How to fork and join

## Fork inside a lifetime

```go
operations := effect.For[Env, AppError]()

program := effect.Scoped(func(scope effect.Scope) effect.Effect[Env, AppError, Result] {
    return operations.Fork(work()).FlatMap(operations.Join)
})
```

`Scoped` gives the child an owner. When the scope closes the child is canceled
and awaited, so it cannot outlive resources that scope holds.

## Await instead of join

`Join` adopts the child's outcome. `Await` hands you the whole `Exit` so you can
decide:

```go
operations.Fork(work()).
    FlatMap(operations.Await).
    Map(func(exit effect.Exit[AppError, Result]) Summary {
        return exit.Fold(summarizeFailure, summarizeSuccess)
    })
```

## Observe from many places

`Await`, `Join` and `Poll` are repeatable and safe for any number of concurrent
observers. A fiber stores its result and broadcasts completion by closing a
channel, so nothing is consumed by the first observer.

## Stop a child and wait for its cleanup

```go
operations.Fork(work()).FlatMap(operations.Interrupt)
```

`Interrupt` returns only once the child has terminated, its scopes have closed
and its finalizers have run. If you only want to signal, close the scope that
owns it.

## Choose the owner deliberately

| Call | Owner |
|---|---|
| `Fork(fx)` / `operations.Fork(fx)` | current dynamic scope |
| `scope.Fork(fx)` / `operations.ForkIn(scope, fx)` | that scope |
| `ForkDaemon(fx)` / `operations.ForkDaemon(fx)` | `Runtime` root scope |

A daemon outlives the `Run` that created it and is bounded by `Runtime.Close`.

## Handle the Never channel

The bare `Fork`, `Await` and `Interrupt` have a `Never` failure channel, which
is precise but does not compose with failing work. Either select your channels
once with `For[R,E]()` and use `operations.Fork`, or widen explicitly:

```go
effect.WidenError[AppError](effect.Fork(work()))
```

`WidenError` is free and total: `Never` is uninhabited, so no typed failure can
exist to translate.

## Forking into a closed scope

It starts no work and fails with an interruption naming `ErrScopeClosed`. Check
for it with `errors.Is` on the interruption's cause.
