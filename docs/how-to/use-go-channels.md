# How to use Go channels

Channels stay native. There is no wrapper type and nothing closes a channel for
you.

## Send and receive

```go
operations := effect.For[R, E]()

operations.Send(requests, request)
operations.Recv[Reply](replies)   // Effect[R, E, Receive[Reply]]
```

Both wait until they can proceed or the effect is interrupted.

## Handle closure

```go
operations.Recv[Record](records).FlatMap(func(received effect.Receive[Record]) effect.Effect[R, E, Summary] {
    if !received.OK {
        return operations.Succeed(running)   // the producer is finished
    }
    return fold(running, received.Value)
})
```

Closure is not a failure in Go, so it is not one here. If your application does
treat it as a domain error:

```go
operations.RecvOrFail(records, StreamError{Reason: "producer finished early"})
```

## Own closure on the producing side

```go
func produce(records chan Record) effect.Effect[R, effect.IOError, effect.Unit] {
    return readAll().
        FlatMap(sendEach(records)).
        Ensuring(effect.Release[R](func(context.Context) error {
            close(records)
            return nil
        }))
}
```

`Ensuring` runs in every outcome, so a failing producer still releases a
consumer blocked on a receive. A send to a closed channel is a **defect**,
because that is what Go's panic already means — so do not send after closing.

## Tell a producer failure from a short stream

Both look like a closed channel to the consumer. Join the producer:

```go
foldRecords(records).FlatMap(func(summary Summary) effect.Effect[R, E, Summary] {
    return operations.Join(producer).As(summary)
})
```

Or wait on both at once with `Done()`:

```go
select {
case reply := <-replies:
case <-producer.Done():
case <-ctx.Done():
}
```

`Done()` is a synchronization signal and never yields the fiber's result; read
that with `Poll`.

## Nil channels

A raw operation on a nil channel blocks forever. `Send` and `Recv` select
alongside cancellation, so they remain interruptible — Go's own `select`
semantics, not a special case.

## Working example

[`examples/pipeline`](../../examples/pipeline/program.go) streams a file's
records through a buffered channel and folds them, joining the producer so a
read failure cannot look like an empty source.
