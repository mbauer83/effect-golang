# Channel reference

Go channels are used as they are, not wrapped in a parallel concurrency
universe. They already provide buffering, backpressure, synchronization, MPMC
operation, directional typing, `select` and ecosystem interoperability.

## Operations

```go
func Send[R, A any](ch chan<- A, value A) Effect[R, Never, Unit]
func Recv[R, A any](ch <-chan A) Effect[R, Never, Receive[A]]
func RecvOrFail[R, E, A any](ch <-chan A, onClosed E) Effect[R, E, A]
```

`Operations[R,E]` provides the pre-widened forms `operations.Send`,
`operations.Recv` and `operations.RecvOrFail`.

## Closure

```go
type Receive[A any] struct {
    Value A
    OK    bool
}
```

Receiving from a closed, drained channel is not exceptional in Go, so `Recv`
reports it as `OK == false` and not as a typed failure. `RecvOrFail` is for
applications that genuinely treat closure as a domain error.

Sending on a closed channel is defined to panic, and Go offers no race-free test
that another goroutine will not close a channel between the test and the send.
`Send` therefore reports that panic as a **defect**, which is what the panic
already means. Closure remains an ownership question: the side responsible for
closing must not send afterwards.

There is no combinator that closes a channel for you. The producer owns closure.

## Nil channels

A raw send or receive on a nil channel blocks forever. Selecting alongside
cancellation makes both effects interruptible, which is Go's own `select`
semantics rather than a special case.

## Interoperability

`Fiber.Done()` is an ordinary receive-only channel, so a caller can wait for a
fiber, its own channels and a context in one `select`:

```go
select {
case reply := <-replies:
case <-fiber.Done():
case <-ctx.Done():
}
```

Selecting on `Done()` alongside a data channel is what keeps a producer's
failure from looking like the end of a short stream. The
[pipeline example](../../examples/pipeline/program.go) does exactly this.

## When a channel is not enough

An effect-specific abstraction is introduced only where it adds semantics native
channels genuinely lack. Three such cases exist, and they are the whole
justification for [`Queue` and `Deferred`](queue.md): shutdown that is safe from
any side, a choice of what a full queue does, and a value that every waiter
observes rather than the first receiver consuming.

`Hub` is a fourth — channels cannot broadcast to a changing set of subscribers
at all — and is not built yet.
