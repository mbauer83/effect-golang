package effect

import (
	"github.com/mbauer83/effect-golang/effect/internal/outcome"
)

// CauseKind identifies one node in a compositional effect failure.
type CauseKind = outcome.CauseKind

// Origin is where a failure came from.
type Origin = outcome.Origin

const (
	// CauseEmpty is the identity for sequential and parallel composition.
	CauseEmpty = outcome.CauseEmpty
	// CauseFailure is an expected, typed failure in the E channel.
	CauseFailure = outcome.CauseFailure
	// CauseDefect is an unexpected panic or explicitly raised defect.
	CauseDefect = outcome.CauseDefect
	// CauseInterrupt is cooperative interruption, normally via context cancellation.
	CauseInterrupt = outcome.CauseInterrupt
	// CauseThen composes failures that happened sequentially.
	CauseThen = outcome.CauseThen
	// CauseBoth composes failures that happened independently in parallel.
	CauseBoth = outcome.CauseBoth
)

// Defect records an unexpected panic value and its stack at the capture point.
type Defect = outcome.Defect

// Interruption records the cancellation cause observed by the runtime.
type Interruption = outcome.Interruption

// Cause preserves the complete reason an effect terminated unsuccessfully.
//
// It is a typed view of the runtime's cause tree. The constructors in this file
// are its only writers, so every failure it contains is an E. Its zero value is
// the empty cause, which is the identity of Then and Both.
type Cause[E any] struct {
	node outcome.Cause
}

// EmptyCause constructs the identity cause.
func EmptyCause[E any]() Cause[E] {
	return Cause[E]{}
}

// FailCause constructs an expected typed failure.
func FailCause[E any](failure E) Cause[E] {
	return Cause[E]{node: outcome.FailCause(failure)}
}

// DieCause constructs an unexpected defect.
func DieCause[E any](defect Defect) Cause[E] {
	return Cause[E]{node: outcome.DieCause(defect)}
}

// InterruptCause constructs a cooperative interruption.
func InterruptCause[E any](reason error) Cause[E] {
	return Cause[E]{node: outcome.InterruptCause(reason)}
}

// Then composes c followed by that. The empty cause is an identity.
func (c Cause[E]) Then(that Cause[E]) Cause[E] {
	return Cause[E]{node: c.node.Then(that.node)}
}

// Both composes independent concurrent causes. The empty cause is an identity.
func (c Cause[E]) Both(that Cause[E]) Cause[E] {
	return Cause[E]{node: c.node.Both(that.node)}
}

// Origin is where this failure came from: the line that produced it and the
// span it was produced inside.
//
// Empty for a composite, whose leaves each have their own, and for a defect,
// which carries a stack instead.
func (c Cause[E]) Origin() Origin {
	return c.node.Origin
}

// Kind returns the node category.
func (c Cause[E]) Kind() CauseKind {
	return c.node.Kind
}

// IsEmpty reports whether c is the composition identity.
func (c Cause[E]) IsEmpty() bool {
	return c.node.IsEmpty()
}

// Failure returns the typed failure only when c is exactly one Fail node.
func (c Cause[E]) Failure() (E, bool) {
	if c.node.Kind != CauseFailure {
		var zero E
		return zero, false
	}
	return asFailure[E](c.node.Failure), true
}

// Defect returns the defect only when c is exactly one Die node.
func (c Cause[E]) Defect() (Defect, bool) {
	return c.node.Defect, c.node.Kind == CauseDefect
}

// Interruption returns the interruption only when c is exactly one Interrupt node.
func (c Cause[E]) Interruption() (Interruption, bool) {
	return c.node.Interruption, c.node.Kind == CauseInterrupt
}

// MapFailure transforms every typed failure while preserving cause structure.
func (c Cause[E]) MapFailure[E2 any](f func(E) E2) Cause[E2] {
	return Cause[E2]{node: eraseFailureTransform(f)(c.node)}
}

// branches returns both children, treating a missing child as the empty cause
// so every traversal stays total on a partially built tree.
func (c Cause[E]) branches() (Cause[E], Cause[E]) {
	return Cause[E]{node: outcome.Child(c.node.Left)},
		Cause[E]{node: outcome.Child(c.node.Right)}
}
