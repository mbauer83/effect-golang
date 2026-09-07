# Resources

This tutorial shows how to acquire something that must be released, and trust
that it will be.

## A lifetime, then a resource

```go
program := effect.Scoped(func(scope effect.Scope) effect.Effect[effect.Unit, effect.IOError, Report] {
    return scope.AcquireRelease(
        io.WriteFile(lockPath, []byte("in progress\n"), 0o600).As(lockPath),
        func(path string) effect.Effect[effect.Unit, effect.Never, effect.Unit] {
            return io.FailuresAsDefects(io.Remove(path))
        },
    ).AndThen(importSources())
})
```

The lock is created before the import starts and removed afterwards — on
success, on failure, on a panic, and on cancellation.

## Why release cannot fail with your error type

One scope may hold a file, a database handle and a listening socket. Their close
errors have no common type, and that type cannot be added to `E` after the
effects are composed. So release has a `Never` failure channel, and a real close
error becomes a **defect** that the cause preserves:

```go
effect.Release[R](func(ctx context.Context) error { return handle.Close() })
effect.FailuresAsDefects(io.Remove(path))
```

Both keep the error visible. Neither discards it.

## Order

Resources are released in reverse acquisition order, and children terminate
first:

```go
database -> transaction -> statement   // acquired
statement -> transaction -> database   // released
```

## Cleanup without a resource

When there is nothing to hold, only something to do afterwards:

```go
importSources().Ensuring(operations.LogInfo("import finished").As(effect.Unit{}))
```

`OnExit` is the same thing with the outcome in hand, so cleanup can distinguish
committing from rolling back.

## What a failing finalizer does

It does not replace your failure. It is appended to it:

```text
Then(Fail(rejected), Die(close failed))
```

## Working example

[`examples/parallelimport`](../../examples/parallelimport/program.go) holds a
scoped lock across a set of concurrent readers. Its end-to-end test checks the
lock is gone after success, after a missing source, and after cancellation.

## Next

- [Manage resources](../how-to/manage-resources.md)
- [Scope reference](../reference/scope.md)
- [Resource safety](../explanation/resource-safety.md)
