package runtime

import "fmt"

// CauseKind identifies one node in a compositional effect failure.
type CauseKind uint8

const (
	// CauseEmpty is the identity for sequential and parallel composition.
	CauseEmpty CauseKind = iota
	// CauseFailure is an expected, typed failure in the E channel.
	CauseFailure
	// CauseDefect is an unexpected panic or explicitly raised defect.
	CauseDefect
	// CauseInterrupted is cooperative interruption, normally via context cancellation.
	CauseInterrupted
	// CauseThen composes failures that happened sequentially.
	CauseThen
	// CauseBoth composes failures that happened independently in parallel.
	CauseBoth
)

// String returns a stable name for a cause kind.
func (kind CauseKind) String() string {
	switch kind {
	case CauseEmpty:
		return "Empty"
	case CauseFailure:
		return "Fail"
	case CauseDefect:
		return "Die"
	case CauseInterrupted:
		return "Interrupt"
	case CauseThen:
		return "Then"
	case CauseBoth:
		return "Both"
	default:
		return fmt.Sprintf("CauseKind(%d)", uint8(kind))
	}
}

// Defect records an unexpected panic value and its stack at the capture point.
// Value must be any because Go permits panic with a value of any type.
type Defect struct {
	Value any
	Stack string
}

// Interruption records the cancellation cause observed by the runtime.
type Interruption struct {
	Cause error
}

// Cause is the erased failure tree carried by the interpreter.
//
// Failure holds the typed failure of the effect that produced this node. The
// interpreter never inspects it: the typed public wrapper is its only writer
// and its only reader, so the dynamic type is always that effect's E channel.
type Cause struct {
	Kind         CauseKind
	Failure      any
	Defect       Defect
	Interruption Interruption
	Left         *Cause
	Right        *Cause
}

// FailCause constructs an expected typed failure.
func FailCause(failure any) Cause {
	return Cause{Kind: CauseFailure, Failure: failure}
}

// DieCause constructs an unexpected defect.
func DieCause(defect Defect) Cause {
	return Cause{Kind: CauseDefect, Defect: defect}
}

// InterruptCause constructs a cooperative interruption.
func InterruptCause(reason error) Cause {
	return Cause{Kind: CauseInterrupted, Interruption: Interruption{Cause: reason}}
}

// IsEmpty reports whether c is the composition identity.
func (c Cause) IsEmpty() bool {
	return c.Kind == CauseEmpty
}

// Then composes c followed by that. An empty cause is the identity.
func (c Cause) Then(that Cause) Cause {
	return c.compose(CauseThen, that)
}

// Both composes independent concurrent causes. An empty cause is the identity.
func (c Cause) Both(that Cause) Cause {
	return c.compose(CauseBoth, that)
}

func (c Cause) compose(kind CauseKind, that Cause) Cause {
	if c.IsEmpty() {
		return that
	}
	if that.IsEmpty() {
		return c
	}
	left, right := c, that
	return Cause{Kind: kind, Left: &left, Right: &right}
}

// Child dereferences one branch, treating a missing branch as empty so every
// traversal remains total on externally constructed values.
func Child(branch *Cause) Cause {
	if branch == nil {
		return Cause{}
	}
	return *branch
}
