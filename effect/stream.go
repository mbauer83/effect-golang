package effect

import (
	"context"
	"slices"
)

// Stream is a description of a finite or infinite sequence of chunks.
//
// Like an Effect, a Stream is a description and not a running computation:
// nothing is produced until a sink runs it. It is pull-based, so a consumer asks
// for the next step and nothing is produced before then -- which is what gives
// backpressure for free, and why no buffer or scheduler is needed to provide it.
//
// A stream's sources are acquired in the consumer's scope, so a file or a hub
// subscription lives exactly as long as the stream that reads it.
type Stream[R, E, A any] struct {
	// open acquires the stream's sources in scope and yields the effect that
	// produces its next step. That effect closes over per-run state, so it is
	// created per run and one Stream value stays reusable -- the same rule that
	// makes a Schedule reusable and its driver not.
	open func(Scope) Effect[R, E, pull[R, E, A]]
}

// pull yields a stream's next step.
type pull[R, E, A any] = Effect[R, E, Step[A]]

// Step is one pull from a stream: a chunk of values, or the end.
//
// Its zero value is the end, so a source that forgets to say so cannot produce
// an infinite stream of nothing.
type Step[A any] struct {
	chunk Chunk[A]
	more  bool
}

// Emit is a step carrying a chunk. An empty chunk is legal: a filter that
// rejected everything it was given produces one, and that is not the end.
func Emit[A any](chunk Chunk[A]) Step[A] {
	return Step[A]{chunk: chunk, more: true}
}

// EndOfStream is the step that says there is nothing more.
func EndOfStream[A any]() Step[A] {
	return Step[A]{}
}

// Chunk returns the step's values and whether the stream continues.
func (step Step[A]) Chunk() (Chunk[A], bool) {
	return step.chunk, step.more
}

// streamFromOpen builds a stream from a function that prepares one run.
func streamFromOpen[R, E, A any](open func(Scope) Effect[R, E, pull[R, E, A]]) Stream[R, E, A] {
	return Stream[R, E, A]{open: open}
}

// streamFromPull builds a stream whose per-run state is prepared without a scope,
// which is the common case for a source that owns no resources.
func streamFromPull[R, E, A any](create func() pull[R, E, A]) Stream[R, E, A] {
	return streamFromOpen(func(Scope) Effect[R, E, pull[R, E, A]] {
		return Suspend(func() Effect[R, E, pull[R, E, A]] {
			return Succeed[R, E](create())
		})
	})
}

// StreamFromSteps builds a stream from a source that produces its own steps.
//
// It is the seam for a source this package does not provide: a socket, a
// database cursor, a driver that reads one batch at a time. Emit and
// EndOfStream are the whole protocol, and a source that can end needs this
// because every other constructor here either knows its values in advance or
// never finishes.
//
// newStep is called once per run, so the state it closes over is per run and
// one Stream value stays reusable -- the same rule that makes a Schedule
// reusable and its driver not.
func StreamFromSteps[R, E, A any](newStep func() Effect[R, E, Step[A]]) Stream[R, E, A] {
	return streamFromPull[R, E, A](func() pull[R, E, A] { return newStep() })
}

// EmptyStream produces nothing.
func EmptyStream[R, E, A any]() Stream[R, E, A] {
	return streamFromPull[R, E, A](func() pull[R, E, A] {
		return Succeed[R, E](EndOfStream[A]())
	})
}

// StreamFail produces nothing and fails.
func StreamFail[R, A, E any](failure E) Stream[R, E, A] {
	return streamFromPull[R, E, A](func() pull[R, E, A] {
		return Fail[R, Step[A]](failure)
	})
}

// StreamOf produces the given values as one chunk.
func StreamOf[R, E, A any](values ...A) Stream[R, E, A] {
	return StreamFromChunks[R, E](ChunkOf(values...))
}

// StreamFromChunks produces the given chunks in order.
func StreamFromChunks[R, E, A any](chunks ...Chunk[A]) Stream[R, E, A] {
	owned := slices.Clone(chunks)
	return streamFromPull[R, E, A](func() pull[R, E, A] {
		remaining := owned
		return From(func(context.Context, R) Exit[E, Step[A]] {
			if len(remaining) == 0 {
				return ExitSuccess[E](EndOfStream[A]())
			}
			head := remaining[0]
			remaining = remaining[1:]
			return ExitSuccess[E](Emit(head))
		})
	})
}

// StreamFromResource acquires a source in the consumer's scope and produces the
// stream that reads it.
//
// This is what the representation's scope is for: the source is released when
// the consumer is finished with it, including when the consumer stopped early,
// failed, or was cancelled. A stream over an open file, a queue it owns, or a
// hub subscription is built with this.
func StreamFromResource[R, E, A, S any](
	acquire func(Scope) Effect[R, E, S],
	read func(S) Stream[R, E, A],
) Stream[R, E, A] {
	return streamFromOpen(func(scope Scope) Effect[R, E, pull[R, E, A]] {
		return acquire(scope).FlatMap(func(source S) Effect[R, E, pull[R, E, A]] {
			return read(source).open(scope)
		})
	})
}

// StreamFromEffect produces exactly one value, from one evaluation.
func StreamFromEffect[R, E, A any](fx Effect[R, E, A]) Stream[R, E, A] {
	return streamFromPull[R, E, A](func() pull[R, E, A] {
		spent := false
		return Suspend(func() pull[R, E, A] {
			if spent {
				return Succeed[R, E](EndOfStream[A]())
			}
			spent = true
			return fx.Map(func(value A) Step[A] {
				return Emit(ChunkOf(value))
			})
		})
	})
}

// StreamRepeatEffect evaluates fx forever, producing one value per evaluation.
// Pair it with TakeStream or a predicate; on its own it never ends.
func StreamRepeatEffect[R, E, A any](fx Effect[R, E, A]) Stream[R, E, A] {
	return streamFromPull[R, E, A](func() pull[R, E, A] {
		return fx.Map(func(value A) Step[A] {
			return Emit(ChunkOf(value))
		})
	})
}

// StreamFromQueue produces a queue's values in batches of at most chunkSize,
// ending when the queue has been shut down and drained.
//
// The queue is not shut down by the stream: whoever created it owns that, which
// is the same ownership rule a channel's producer follows.
func StreamFromQueue[R, E, A any](queue Queue[A], chunkSize int) Stream[R, E, A] {
	return streamFromPull[R, E, A](func() pull[R, E, A] {
		return WidenError[E](queue.TakeUpTo[R](chunkSize)).Map(batchStep[A])
	})
}

// StreamFromSubscription produces a hub subscription's values in batches of at
// most chunkSize, ending when the subscription has ended and drained.
func StreamFromSubscription[R, E, A any](subscription Subscription[A], chunkSize int) Stream[R, E, A] {
	return streamFromPull[R, E, A](func() pull[R, E, A] {
		return WidenError[E](subscription.TakeUpTo[R](chunkSize)).Map(batchStep[A])
	})
}

// batchStep reads an empty batch as the end of the stream, which is what the
// blocking batch operations mean by it: they wait for at least one value, so
// nothing coming back means there will be nothing more.
func batchStep[A any](batch []A) Step[A] {
	if len(batch) == 0 {
		return EndOfStream[A]()
	}
	return Emit(Chunk[A]{values: batch})
}

// Operations carries these channels into the stream sources below, whose
// requirement and failure channels cannot be inferred from their arguments.

// StreamOf produces the given values as one chunk, in these channels.
func (Operations[R, E]) StreamOf[A any](values ...A) Stream[R, E, A] {
	return StreamOf[R, E](values...)
}

// StreamFromChunks produces the given chunks in order, in these channels.
func (Operations[R, E]) StreamFromChunks[A any](chunks ...Chunk[A]) Stream[R, E, A] {
	return StreamFromChunks[R, E](chunks...)
}

// EmptyStream produces nothing, in these channels.
func (Operations[R, E]) EmptyStream[A any]() Stream[R, E, A] {
	return EmptyStream[R, E, A]()
}

// StreamFromQueue produces a queue's values in batches, in these channels.
func (Operations[R, E]) StreamFromQueue[A any](queue Queue[A], chunkSize int) Stream[R, E, A] {
	return StreamFromQueue[R, E](queue, chunkSize)
}

// StreamFromSubscription produces a subscription's values in batches, in these
// channels.
func (Operations[R, E]) StreamFromSubscription[A any](
	subscription Subscription[A],
	chunkSize int,
) Stream[R, E, A] {
	return StreamFromSubscription[R, E](subscription, chunkSize)
}
