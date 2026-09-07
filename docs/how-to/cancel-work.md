# How to cancel work

## Cancel from outside

Cancel the context you passed to `Run`, preferably with a reason:

```go
ctx, cancel := context.WithCancelCause(context.Background())
cancel(errors.New("operator requested stop"))
```

The reason survives into the `Exit`:

```go
cause, _ := exit.Cause()
interruption, ok := cause.Interruption()
errors.Is(interruption.Cause, stop) // true
```

## Cancel one child

```go
operations.Interrupt(fiber)
```

It returns only after the child's cleanup has run. To cancel a whole subtree,
close the scope that owns it — leaving `Scoped` does that.

## Tell why the runtime canceled something

```go
switch {
case errors.Is(interruption.Cause, effect.ErrTimedOut):
case errors.Is(interruption.Cause, effect.ErrSiblingFailed):
case errors.Is(interruption.Cause, effect.ErrScopeClosed):
case errors.Is(interruption.Cause, effect.ErrRuntimeClosed):
}
```

The full list is in the [interruption reference](../reference/interruption.md).

## Make your own effect cooperate

Cancellation is cooperative. An effect that never observes its context cannot be
stopped:

```go
operations.From(func(ctx context.Context, env Env) effect.Exit[AppError, Result] {
    select {
    case result := <-work:
        return effect.ExitSuccess[AppError](result)
    case <-ctx.Done():
        return effect.ExitCause[AppError, Result](
            effect.InterruptCause[AppError](context.Cause(ctx)),
        )
    }
})
```

In a CPU-bound loop, insert checkpoints instead:

```go
operations.CheckInterrupt().AndThen(nextChunk())
```

## Do not confuse interruption with failure

Interruption never widens `E`. Use `IsInterruptedOnly` rather than inspecting a
failure that is not there:

```go
if cause.IsInterruptedOnly() {
    // stopped, not broken
}
```

## Make cleanup survive it

Finalizers already run with a context detached from cancellation, so an
`Ensuring` block or a scoped release is not skipped by a canceled caller.
