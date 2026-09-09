package effect

// A transform wraps a stream's pull rather than its values, so it inherits the
// stream's resource acquisition and its backpressure without knowing about
// either. Every transform whose element type changes is a package function,
// because a method returning its receiver's type with a fresh argument hits
// Go's instantiation-cycle check (golang/go#80172).

// MapStream transforms every value.
func MapStream[R, E, A, B any](stream Stream[R, E, A], transform func(A) B) Stream[R, E, B] {
	return MapStreamChunks(stream, func(chunk Chunk[A]) Chunk[B] {
		return MapChunk(chunk, transform)
	})
}

// MapStreamChunks transforms whole chunks, which is how a transform avoids
// paying per value for work it can do in a batch.
func MapStreamChunks[R, E, A, B any](
	stream Stream[R, E, A],
	transform func(Chunk[A]) Chunk[B],
) Stream[R, E, B] {
	return steppingStream(stream, func(step Step[A]) Step[B] {
		chunk, more := step.Chunk()
		if !more {
			return EndOfStream[B]()
		}
		return Emit(transform(chunk))
	})
}

// MapStreamEffect transforms every value with an effect, evaluating them in
// order. Concurrent per-element work is deliberately absent for now; fork it
// explicitly if that is what you want.
func MapStreamEffect[R, E, A, B any](
	stream Stream[R, E, A],
	transform func(A) Effect[R, E, B],
) Stream[R, E, B] {
	return streamFromOpen(func(scope Scope) Effect[R, E, pull[R, E, B]] {
		return stream.open(scope).Map(func(next pull[R, E, A]) pull[R, E, B] {
			return next.FlatMap(func(step Step[A]) pull[R, E, B] {
				chunk, more := step.Chunk()
				if !more {
					return Succeed[R, E](EndOfStream[B]())
				}
				return ForEach(chunk.Values(), transform).Map(func(values []B) Step[B] {
					return Emit(Chunk[B]{values: values})
				})
			})
		})
	})
}

// CollectStreamEffect transforms every value with an effect that produces some
// number of values, so a value can be dropped or expanded rather than only
// replaced.
//
// It is the transform MapStreamEffect is not: mapping is one-for-one, and a
// consumer that has to decide effectfully whether a value survives -- a queue
// discarding a message it cannot read, a parser skipping a line it cannot
// parse -- has nowhere to put the decision. An empty chunk drops the value; a
// chunk of one replaces it; a longer one expands it.
//
// The values are collected into the chunk the source produced, so a chunk that
// loses every value becomes an empty chunk rather than the end of the stream.
func CollectStreamEffect[R, E, A, B any](
	stream Stream[R, E, A],
	transform func(A) Effect[R, E, Chunk[B]],
) Stream[R, E, B] {
	return MapStreamChunks(MapStreamEffect(stream, transform),
		func(chunk Chunk[Chunk[B]]) Chunk[B] {
			collected := make([]B, 0, chunk.Len())
			for _, values := range chunk.Values() {
				collected = append(collected, values.Values()...)
			}
			return Chunk[B]{values: collected}
		})
}

// MapStreamError transforms a stream's typed failure, which is what lets a
// stream produced by one layer be consumed by another whose failure channel is
// its own.
//
// It maps the acquisition and the pull alike, because either can fail: a source
// that could not be opened and a source that stopped mid-way are both failures
// of the stream, and a consumer that only saw one of them would be surprised by
// the other.
func MapStreamError[R, E, E2, A any](stream Stream[R, E, A], transform func(E) E2) Stream[R, E2, A] {
	return streamFromOpen(func(scope Scope) Effect[R, E2, pull[R, E2, A]] {
		return stream.open(scope).MapError(transform).
			Map(func(next pull[R, E, A]) pull[R, E2, A] {
				return next.MapError(transform)
			})
	})
}

// FilterStream keeps the values predicate accepts. A chunk that loses every
// value becomes an empty chunk rather than the end of the stream.
func (stream Stream[R, E, A]) FilterStream(keep func(A) bool) Stream[R, E, A] {
	return MapStreamChunks(stream, func(chunk Chunk[A]) Chunk[A] {
		return chunk.Filter(keep)
	})
}

// TakeStream ends the stream after count values.
//
// It ends the stream rather than draining it, so taking from an infinite source
// terminates, and the source's resources are released when the consumer's scope
// closes.
func (stream Stream[R, E, A]) TakeStream(count int) Stream[R, E, A] {
	return stagedStream(stream, func() streamStage[A] {
		remaining := count
		return streamStage[A]{
			Ended: func() bool { return remaining < 1 },
			Rewrite: func(step Step[A]) Step[A] {
				chunk, more := step.Chunk()
				// The budget is checked here as well as in the gate. Emitting an
				// empty chunk instead would not end the stream, so relying on
				// the gate alone would make termination depend on one branch.
				if !more || remaining < 1 {
					return EndOfStream[A]()
				}
				taken := chunk.TakeFirst(remaining)
				remaining -= taken.Len()
				return Emit(taken)
			},
		}
	})
}

// DropStream discards the first count values.
func (stream Stream[R, E, A]) DropStream(count int) Stream[R, E, A] {
	return stagedStream(stream, func() streamStage[A] {
		remaining := count
		return streamStage[A]{
			Ended: withoutEnd,
			Rewrite: func(step Step[A]) Step[A] {
				chunk, more := step.Chunk()
				if !more || remaining < 1 {
					return step
				}
				keptValue := chunk.DropFirst(remaining)
				remaining -= chunk.Len() - keptValue.Len()
				return Emit(keptValue)
			},
		}
	})
}

// TakeStreamWhile ends the stream at the first value predicate rejects.
func (stream Stream[R, E, A]) TakeStreamWhile(keep func(A) bool) Stream[R, E, A] {
	return stagedStream(stream, func() streamStage[A] {
		ended := false
		return streamStage[A]{
			Ended: func() bool { return ended },
			Rewrite: func(step Step[A]) Step[A] {
				chunk, more := step.Chunk()
				if !more || ended {
					return EndOfStream[A]()
				}
				keptValue, rejected := chunk.TakeWhile(keep)
				ended = rejected
				return Emit(keptValue)
			},
		}
	})
}

// ConcatStreams produces the second stream's values after the first ends. Both
// streams' sources are acquired in the consumer's scope, so neither outlives it.
func ConcatStreams[R, E, A any](first Stream[R, E, A], second Stream[R, E, A]) Stream[R, E, A] {
	return streamFromOpen(func(scope Scope) Effect[R, E, pull[R, E, A]] {
		return Zip(first.open(scope), second.open(scope)).Map(
			func(pulls Product[pull[R, E, A], pull[R, E, A]]) pull[R, E, A] {
				return concatPulls(pulls.First, pulls.Second)
			},
		)
	})
}

func concatPulls[R, E, A any](first pull[R, E, A], second pull[R, E, A]) pull[R, E, A] {
	drained := false
	return Suspend(func() pull[R, E, A] {
		if drained {
			return second
		}
		return first.FlatMap(func(step Step[A]) pull[R, E, A] {
			if _, more := step.Chunk(); more {
				return Succeed[R, E](step)
			}
			drained = true
			return second
		})
	})
}

// steppingStream rewrites each step with a stateless transform.
func steppingStream[R, E, A, B any](
	stream Stream[R, E, A],
	rewrite func(Step[A]) Step[B],
) Stream[R, E, B] {
	return streamFromOpen(func(scope Scope) Effect[R, E, pull[R, E, B]] {
		return stream.open(scope).Map(func(next pull[R, E, A]) pull[R, E, B] {
			return next.Map(rewrite)
		})
	})
}

// streamStage rewrites a stream's steps for one run.
//
// Ended is consulted before pulling, which is what stops a finished stage from
// making its source produce a value nobody will use. That is not an
// optimisation: taking three values from a blocking source must not wait for a
// fourth, and taking three from an effect must not evaluate it a fourth time.
type streamStage[A any] struct {
	Ended   func() bool
	Rewrite func(Step[A]) Step[A]
}

func withoutEnd() bool {
	return false
}

// stagedStream applies a stage that carries per-run state, built once per run so
// one Stream value stays reusable.
func stagedStream[R, E, A any](
	stream Stream[R, E, A],
	newStage func() streamStage[A],
) Stream[R, E, A] {
	return streamFromOpen(func(scope Scope) Effect[R, E, pull[R, E, A]] {
		return stream.open(scope).Map(func(next pull[R, E, A]) pull[R, E, A] {
			stage := newStage()
			return Suspend(func() pull[R, E, A] {
				if stage.Ended() {
					return Succeed[R, E](EndOfStream[A]())
				}
				return next.Map(stage.Rewrite)
			})
		})
	})
}
