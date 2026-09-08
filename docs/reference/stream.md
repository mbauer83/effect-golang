# Stream reference

`Stream[R, E, A]` describes a finite or infinite sequence of chunks.

Like an `Effect`, a `Stream` is a description and not a running computation:
nothing is produced until a sink runs it.

## Representation

```text
Stream[R,E,A]  =  open(Scope) -> Effect[R,E, next]
                  where next : Effect[R,E, Step[A]]
```

Two properties of this shape are load-bearing, and neither could be added
later without changing every signature.

**It is chunked.** A stream moves `Chunk[A]`, not `A`. Per-value plumbing would
cost an interpretation per element, and every transform and sink would have the
wrong type.

**It is pull-based.** A consumer asks for the next step and nothing is produced
before then, which is where backpressure comes from. A push-based stream needs
somewhere to put values a consumer is not ready for — a buffer with a policy, or
a scheduler, and [the runtime does not add a scheduler](../explanation/go-runtime-model.md).

`open` acquires the stream's sources in the consumer's scope, so a file or a hub
subscription lives exactly as long as the stream reading it. It yields the effect
that produces the next step; interpreting that effect repeatedly walks the
stream. That effect holds per-run state and is created per run, which is what
keeps one `Stream` value reusable — the same rule that makes a `Schedule`
reusable and its driver not.

## Step

```go
func Emit[A any](chunk Chunk[A]) Step[A]
func EndOfStream[A any]() Step[A]
func (step Step[A]) Chunk() (Chunk[A], bool)
```

`Step[A]`'s zero value is the end, so a source that forgets to say so cannot
produce an infinite stream of nothing.

**An empty chunk is not the end.** A filter that rejected everything it was
given produces one, and reading that as the end would silently truncate the
stream at the first fully-rejected chunk.

## Chunk

```go
func ChunkOf[A any](values ...A) Chunk[A]
func MapChunk[A, B any](chunk Chunk[A], transform func(A) B) Chunk[B]
func FoldChunk[A, S any](chunk Chunk[A], state S, combine func(S, A) S) S

func (chunk Chunk[A]) Len() int
func (chunk Chunk[A]) IsEmpty() bool
func (chunk Chunk[A]) Values() []A
func (chunk Chunk[A]) At(index int) A
func (chunk Chunk[A]) Filter(keep func(A) bool) Chunk[A]
func (chunk Chunk[A]) TakeFirst(count int) Chunk[A]
func (chunk Chunk[A]) DropFirst(count int) Chunk[A]
func (chunk Chunk[A]) TakeWhile(keep func(A) bool) (Chunk[A], bool)
```

`ChunkOf` copies, so a source that reuses a buffer cannot corrupt a chunk it
already emitted. `Values` returns the chunk's own slice; treat it as read-only.

## Sources

```go
func StreamOf[R, E, A any](values ...A) Stream[R, E, A]
func StreamFromChunks[R, E, A any](chunks ...Chunk[A]) Stream[R, E, A]
func EmptyStream[R, E, A any]() Stream[R, E, A]
func StreamFail[R, A, E any](failure E) Stream[R, E, A]
func StreamFromEffect[R, E, A any](fx Effect[R, E, A]) Stream[R, E, A]
func StreamRepeatEffect[R, E, A any](fx Effect[R, E, A]) Stream[R, E, A]
func StreamFromQueue[R, E, A any](queue Queue[A], chunkSize int) Stream[R, E, A]
func StreamFromSubscription[R, E, A any](s Subscription[A], chunkSize int) Stream[R, E, A]
func StreamFromResource[R, E, A, S any](
    acquire func(Scope) Effect[R, E, S],
    read func(S) Stream[R, E, A],
) Stream[R, E, A]
```

`StreamFromResource` is what the representation's scope is for: build a stream
over an open file, a queue it owns, or a subscription, and the source is
released when the consumer is finished with it.

`StreamRepeatEffect` never ends on its own; pair it with `TakeStream` or
`TakeStreamWhile`.

A stream built over a `Queue` does not shut that queue down. Whoever created it
owns that, which is the same ownership rule a channel's producer follows.

`StreamFromSteps` is the seam for a source this package does not provide: a
socket, a database cursor, a driver that reads one batch at a time. `Emit` and
`EndOfStream` are the whole protocol, and a source that can *end* needs it,
because every other constructor here either knows its values in advance or never
finishes. Its `newStep` is called once per run, so the state it closes over is
per run and one `Stream` value stays reusable.

`Operations[R, E]` carries the channels for the sources whose arguments cannot
imply them: `operations.StreamOf`, `StreamFromChunks`, `EmptyStream`,
`StreamFromQueue`, `StreamFromSubscription`.

## Transforms

```go
func MapStream[R, E, A, B any](s Stream[R, E, A], transform func(A) B) Stream[R, E, B]
func MapStreamChunks[R, E, A, B any](s Stream[R, E, A], transform func(Chunk[A]) Chunk[B]) Stream[R, E, B]
func MapStreamEffect[R, E, A, B any](s Stream[R, E, A], transform func(A) Effect[R, E, B]) Stream[R, E, B]
func MapStreamError[R, E, E2, A any](s Stream[R, E, A], transform func(E) E2) Stream[R, E2, A]
func ConcatStreams[R, E, A any](first Stream[R, E, A], second Stream[R, E, A]) Stream[R, E, A]

func (stream Stream[R, E, A]) FilterStream(keep func(A) bool) Stream[R, E, A]
func (stream Stream[R, E, A]) TakeStream(count int) Stream[R, E, A]
func (stream Stream[R, E, A]) DropStream(count int) Stream[R, E, A]
func (stream Stream[R, E, A]) TakeStreamWhile(keep func(A) bool) Stream[R, E, A]
```

A transform wraps the stream's pull rather than its values, so it inherits both
the resource acquisition and the backpressure without knowing about either.
Transforms whose element type changes are package functions, because a method
returning its receiver's type with a fresh argument hits Go's
instantiation-cycle check.

`TakeStream` and `TakeStreamWhile` end the stream rather than draining it, and
they check before pulling. That is not an optimisation: taking three values from
a blocking source must not wait for a fourth, and taking three from an effect
must not evaluate it a fourth time.

`MapStreamEffect` evaluates in order. Concurrent per-element work is not here;
fork it explicitly when that is what you want.

`MapStreamError` is what lets a stream produced by one layer be consumed by
another whose failure channel is its own: a transport's stream fails with the
transport's fault, and the application adapts it as it would adapt an effect's.
It maps the acquisition and the pull alike, because either can fail -- a source
that could not be opened and a source that stopped mid-way are both failures of
the stream, and a consumer that only saw one of them would be surprised by the
other.

## Sinks

```go
func RunFold[R, E, A, S any](s Stream[R, E, A], initial S, combine func(S, A) S) Effect[R, E, S]
func RunCollect[R, E, A any](s Stream[R, E, A]) Effect[R, E, []A]
func RunCount[R, E, A any](s Stream[R, E, A]) Effect[R, E, int]
func RunDrain[R, E, A any](s Stream[R, E, A]) Effect[R, E, Unit]
func RunForEach[R, E, A any](s Stream[R, E, A], visit func(A) Effect[R, E, Unit]) Effect[R, E, Unit]
func RunIntoQueue[R, E, A any](s Stream[R, E, A], queue Queue[A]) Effect[R, E, Unit]
func RunIntoHub[R, E, A any](s Stream[R, E, A], hub Hub[A]) Effect[R, E, Unit]
```

A sink opens the stream's sources in a scope of its own, pulls until the stream
ends, and closes that scope — so a source is released whatever the outcome:
completion, an early `TakeStream`, a failing visitor, or cancellation.

The loop is a `FlatMap` recursion, so it inherits the interpreter's stack
safety: consuming a million values adds no frames.

`RunCollect` holds everything; use `RunFold` or `RunForEach` for a stream that
does not fit in memory. `RunIntoQueue` and `RunIntoHub` shut their destination
down when the stream ends, which is how a consumer of that queue or hub learns
there will be nothing more — and why a queue's shutdown has to be safe from
this side.

## Deliberately absent

Merging and interleaving streams, broadcasting one stream to several consumers,
grouping and windowing, concurrent per-element effects, and pipes as
first-class composable values.

Each is expressible on the representation above and none of them constrains it.
Building them before there is a caller would fossilise guesses about their
shape, for the same reason the
[code generator](../explanation/sequencing-in-go.md) is held back.

## See also

- [Queue](queue.md) and [Hub](hub.md), the structures a stream bridges to.
- [Channels](channels.md), which remain the first thing to reach for.
