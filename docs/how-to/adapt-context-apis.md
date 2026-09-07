# How to adapt context-aware Go APIs

## Adapt the (A, error) shape

```go
rows := effect.Try(
    func(ctx context.Context, env Env) (*sql.Rows, error) {
        return env.DB.QueryContext(ctx, query, id)
    },
    func(err error) AppError { return AppError{Reason: err.Error()} },
)
```

The context is handed to you first, exactly as an ordinary Go API expects. Pass
it through rather than storing it.

## Keep the platform error inspectable

Classify without flattening to a string, so `errors.Is` and `errors.As` keep
working:

```go
func classify(err error) AppError {
    return AppError{Op: "query", Err: err}
}

func (failure AppError) Unwrap() error { return failure.Err }
```

`IOError` is the built-in example: it records the operation, one or two paths,
and the wrapped platform error.

## Own a resource the API returns

```go
effect.Scoped(func(scope effect.Scope) effect.Effect[Env, AppError, Report] {
    return scope.
        AcquireRelease(beginTransaction(), rollbackOrCommit).
        FlatMap(runQueries)
})
```

Acquisition stays context-aware: cancellation is not stripped from it, so
`QueryContext`, `DialContext` and `NewRequestWithContext` behave normally. If
acquisition is interrupted before it returns a resource, cleaning up partial
state is the acquisition's job — the conventional Go contract.

## Adapt an API that cannot be canceled

Most of `os` takes no context. Check on both sides and do not move the call to
an unowned goroutine to fake promptness:

```go
operations.From(func(ctx context.Context, env Env) effect.Exit[AppError, []byte] {
    if reason := context.Cause(ctx); reason != nil {
        return effect.ExitCause[AppError, []byte](effect.InterruptCause[AppError](reason))
    }
    data, err := os.ReadFile(path)
    ...
})
```

The built-in `FileSystem` capability does exactly this, which is also why a
context-aware filesystem implementation can cooperate more strongly.

## Give a callback-based API a fiber

```go
effect.Scoped(func(scope effect.Scope) effect.Effect[Env, AppError, Result] {
    events := make(chan Event, 16)
    return operations.Fork(publish(events)).
        FlatMap(func(publisher effect.Fiber[AppError, effect.Unit]) effect.Effect[Env, AppError, Result] {
            return consume(events, publisher)
        })
})
```

The scope owns the publisher, so it cannot outlive the consumer's lifetime.

## Do not smuggle runtime state through context.Value

The runtime passes its own services explicitly. When adapting to an ecosystem
tracer that expects a context value, derive that value at the external API
boundary; the effect runtime keeps its own representation.
