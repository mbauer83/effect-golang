package effect

import (
	"context"
	"sync"
)

// Ref is a mutable cell whose operations are effects, one at a time.
//
// It is the piece that was missing between a plain variable and a Queue. A
// Queue hands values from one fiber to another and a Deferred broadcasts one
// result; neither is the right shape for state that is simply read and
// written, and every program that needed that was writing a struct with a
// mutex and wrapping the calls by hand.
//
// A Ref is created by an effect rather than by a constructor, for the reason a
// Deferred is: state that existed before interpretation would be shared
// between runs and between the attempts of a retry, which is never what the
// program meant.
//
// It is not a transaction. One operation on one Ref is atomic; two operations,
// or two Refs, are not. Where several values have to move together, put them in
// one Ref as one value and change it with Modify -- which is the answer in
// almost every case, and the reason this is not the beginning of an STM.
type Ref[A any] struct {
	state *cell[A]
}

// cell is the Ref's state, behind a pointer so that copying a Ref copies the
// handle and not the value it refers to.
type cell[A any] struct {
	mutex sync.Mutex
	value A
}

// NewRef creates a cell holding the given value.
func NewRef[R, A any](initial A) Effect[R, Never, Ref[A]] {
	return From(func(context.Context, R) Exit[Never, Ref[A]] {
		return ExitSuccess[Never](Ref[A]{state: &cell[A]{value: initial}})
	})
}

// Get reads the current value.
func (ref Ref[A]) Get[R any]() Effect[R, Never, A] {
	return From(func(context.Context, R) Exit[Never, A] {
		ref.state.mutex.Lock()
		defer ref.state.mutex.Unlock()
		return ExitSuccess[Never](ref.state.value)
	})
}

// Set replaces the value.
func (ref Ref[A]) Set[R any](value A) Effect[R, Never, Unit] {
	return ref.Update[R](func(A) A { return value })
}

// Update derives the next value from the current one.
//
// The read and the write are one step, so two fibers updating at once both take
// effect: this is what a plain variable cannot promise and what the mutex
// around one used to be for.
func (ref Ref[A]) Update[R any](change func(A) A) Effect[R, Never, Unit] {
	return Modify[R](ref, func(current A) (A, Unit) {
		return change(current), Unit{}
	})
}

// GetAndSet replaces the value and reports what it was.
func (ref Ref[A]) GetAndSet[R any](value A) Effect[R, Never, A] {
	return ref.GetAndUpdate[R](func(A) A { return value })
}

// GetAndUpdate changes the value and reports what it was.
func (ref Ref[A]) GetAndUpdate[R any](change func(A) A) Effect[R, Never, A] {
	return Modify[R](ref, func(current A) (A, A) {
		return change(current), current
	})
}

// UpdateAndGet changes the value and reports what it became.
func (ref Ref[A]) UpdateAndGet[R any](change func(A) A) Effect[R, Never, A] {
	return Modify[R](ref, func(current A) (A, A) {
		next := change(current)
		return next, next
	})
}

// Modify changes the value and derives a result from what it was.
//
// It is the one operation the others are written in terms of, because it is the
// only one that can decide and write in the same step: a check followed by an
// update is two operations and two fibers can interleave between them, and this
// is how that is avoided.
//
// change is applied exactly once. A lock-free cell would retry on contention
// and so would apply it more than once, which is only safe for a change that
// happens to be pure -- and nothing in Go can promise that one is.
//
// It is a package function because a method cannot introduce the result type
// this one derives.
func Modify[R, A, B any](ref Ref[A], change func(A) (A, B)) Effect[R, Never, B] {
	return From(func(context.Context, R) Exit[Never, B] {
		ref.state.mutex.Lock()
		defer ref.state.mutex.Unlock()
		next, derived := change(ref.state.value)
		ref.state.value = next
		return ExitSuccess[Never](derived)
	})
}
