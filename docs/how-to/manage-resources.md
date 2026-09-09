# How to manage resources

## Acquire and release

```go
effect.Scoped(func(scope effect.Scope) effect.Effect[R, E, A] {
    return scope.
        AcquireRelease(open(), closeIt).
        FlatMap(use)
})
```

Release runs exactly once per successful acquisition, in reverse acquisition
order, after every fiber the scope owns has terminated.

## Convert a Close error

Release cannot fail with `E`. Pick one deliberately:

```go
// A defect, so the error is preserved in the cause.
effect.Release[R](func(ctx context.Context) error { return handle.Close() })

// The same, for an effect you already have.
effect.OrDie(io.Remove(path))

// Absorbed on purpose, having decided that is correct.
operations.LogWarn("could not remove lock").As(effect.Unit{})
```

## Clean up without a resource

```go
transfer().Ensuring(releaseLease())

transfer().OnExit(func(exit effect.Exit[E, A]) effect.Effect[R, effect.Never, effect.Unit] {
    if exit.IsSuccess() {
        return commit()
    }
    return rollback()
})
```

## Choose where to put Scoped when retrying

```go
// Each attempt owns and releases its own resources.
effect.Scoped(useResource).Retry(policy)

// All attempts share one lifetime; failed attempts keep their registrations
// until it closes.
effect.Scoped(func(scope effect.Scope) effect.Effect[R, E, A] {
    return useResource.Retry(policy)
})
```

The first is the default for an acquire-use workflow.

## Give a layer's service a consumer lifetime

```go
connection := effect.LayerScoped(func(scope effect.Scope) effect.Effect[effect.Unit, DBError, Conn] {
    return scope.AcquireRelease(dial(), closeConn)
})

program := query().ProvideLayerSame(connection)
```

The scope handed to `build` belongs to the effect the layer is provided to, so
the connection is closed after the query, not when construction returned.

## Check for leaks in a test

```go
runtime, _ := effect.NewRuntime(effect.WithDebugTracking())
runtime.Run(ctx, env, program)

if live := runtime.LiveWork(); !live.IsEmpty() {
    t.Fatalf("program left work behind: %#v", live)
}
```

`Close` also reports any remainder to the diagnostics sink.
