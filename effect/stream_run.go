package effect

// Running a stream is where it stops being a description. A sink opens the
// stream's sources in a scope of its own, pulls until the stream ends, and
// closes that scope -- so a source's file, queue or subscription is released
// when the consumer is finished with it, whatever the outcome.
//
// The loop is a FlatMap recursion, so it inherits the interpreter's stack
// safety: consuming a million values adds no frames.

// RunFold accumulates over every value the stream produces.
func RunFold[R, E, A, S any](
	stream Stream[R, E, A],
	initial S,
	combine func(S, A) S,
) Effect[R, E, S] {
	return Scoped(func(scope Scope) Effect[R, E, S] {
		return stream.open(scope).FlatMap(func(next pull[R, E, A]) Effect[R, E, S] {
			return foldSteps(next, initial, combine)
		})
	})
}

func foldSteps[R, E, A, S any](
	next pull[R, E, A],
	state S,
	combine func(S, A) S,
) Effect[R, E, S] {
	return next.FlatMap(func(step Step[A]) Effect[R, E, S] {
		chunk, more := step.Chunk()
		if !more {
			return Succeed[R, E](state)
		}
		return foldSteps(next, FoldChunk(chunk, state, combine), combine)
	})
}

// RunCollect gathers every value into one slice. It is for a stream you know is
// finite and small enough to hold; use RunFold or RunForEach otherwise.
func RunCollect[R, E, A any](stream Stream[R, E, A]) Effect[R, E, []A] {
	return RunFold(stream, []A(nil), func(values []A, value A) []A {
		return append(values, value)
	})
}

// RunCount reports how many values the stream produced, without holding them.
func RunCount[R, E, A any](stream Stream[R, E, A]) Effect[R, E, int] {
	return RunFold(stream, 0, func(count int, _ A) int {
		return count + 1
	})
}

// RunDrain pulls the stream to its end and discards its values, which is what a
// stream whose transforms already did the work needs.
func RunDrain[R, E, A any](stream Stream[R, E, A]) Effect[R, E, Unit] {
	return RunFold(stream, Unit{}, func(unit Unit, _ A) Unit {
		return unit
	})
}

// RunForEach evaluates an effect for every value, in order, stopping at the
// first failure.
func RunForEach[R, E, A any](stream Stream[R, E, A], visit func(A) Effect[R, E, Unit]) Effect[R, E, Unit] {
	return Scoped(func(scope Scope) Effect[R, E, Unit] {
		return stream.open(scope).FlatMap(func(next pull[R, E, A]) Effect[R, E, Unit] {
			return visitSteps(next, visit)
		})
	})
}

func visitSteps[R, E, A any](next pull[R, E, A], visit func(A) Effect[R, E, Unit]) Effect[R, E, Unit] {
	return next.FlatMap(func(step Step[A]) Effect[R, E, Unit] {
		chunk, more := step.Chunk()
		if !more {
			return Succeed[R, E](Unit{})
		}
		return ForEach(chunk.Values(), visit).FlatMap(func([]Unit) Effect[R, E, Unit] {
			return visitSteps(next, visit)
		})
	})
}

// RunIntoQueue offers every value to a queue, then shuts it down so a consumer
// reading the queue learns the stream has ended.
//
// It is the bridge from a stream to work that wants to pull at its own pace, and
// it is the reason the queue's shutdown has to be safe from this side.
func RunIntoQueue[R, E, A any](stream Stream[R, E, A], queue Queue[A]) Effect[R, E, Unit] {
	return RunForEach(stream, func(value A) Effect[R, E, Unit] {
		return WidenError[E](queue.Offer[R](value)).As(Unit{})
	}).Ensuring(OrDie(queue.Shutdown[R]()))
}

// RunIntoHub publishes every value to a hub, then shuts it down so every
// subscriber learns the stream has ended.
func RunIntoHub[R, E, A any](stream Stream[R, E, A], hub Hub[A]) Effect[R, E, Unit] {
	return RunForEach(stream, func(value A) Effect[R, E, Unit] {
		return WidenError[E](hub.Publish[R](value)).As(Unit{})
	}).Ensuring(OrDie(hub.Shutdown[R]()))
}
