package outcome

import (
	"fmt"
	"runtime/debug"
)

// CauseKind identifies one node in a compositional effect failure.
type CauseKind uint8

const (
	// CauseEmpty is the identity for sequential and parallel composition.
	CauseEmpty CauseKind = iota
	// CauseFailure is an expected, typed failure in the E channel.
	CauseFailure
	// CauseDefect is an unexpected panic or explicitly raised defect.
	CauseDefect
	// CauseInterrupt is cooperative interruption, normally via context cancellation.
	CauseInterrupt
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
	case CauseInterrupt:
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
	Origin       Origin
	Left         *Cause
	Right        *Cause
}

// Origin is where a failure came from.
//
// A typed failure carries no stack, deliberately: it is an expected outcome
// and not a crash, so paying for a stack at every one would be paying for a
// crash report at every 404. What it needs instead is the two things a reader
// actually asks -- which line produced this, and what was going on at the
// time -- and both are one string each.
type Origin struct {
	// Source is the file and line that produced the failure.
	Source string
	// Operation is the innermost named span it was produced inside, which is
	// what says which request or which stage rather than which line.
	Operation string
}

// IsKnown reports whether anything is known about where a failure came from.
func (origin Origin) IsKnown() bool {
	return origin.Source != "" || origin.Operation != ""
}

// String renders where a failure came from, for a reader who wants to open it.
func (origin Origin) String() string {
	switch {
	case origin.Source != "" && origin.Operation != "":
		return origin.Source + " in " + origin.Operation
	case origin.Source != "":
		return origin.Source
	default:
		return origin.Operation
	}
}

// WithOrigin is this cause with where it came from recorded, on the leaves that
// do not have it yet.
//
// Only where it is missing, because a failure is raised once and travels: a
// boundary that translated a store's fault into the domain's did not move the
// line it happened on, and overwriting it with the line of the translation
// would point a reader at the adapter instead of at the cause.
func (c Cause) WithOrigin(origin Origin) Cause {
	if !origin.IsKnown() {
		return c
	}
	switch c.Kind {
	case CauseEmpty:
		return c
	case CauseThen, CauseBoth:
		result := c
		if c.Left != nil {
			left := c.Left.WithOrigin(origin)
			result.Left = &left
		}
		if c.Right != nil {
			right := c.Right.WithOrigin(origin)
			result.Right = &right
		}
		return result
	default:
		c.Origin = c.Origin.fillFrom(origin)
		return c
	}
}

// fillFrom is this with whatever it does not know taken from that.
//
// Field by field rather than all or nothing, because the two halves are
// learned in different places: the line is known where the failure is written
// and the span only when it is run. An all-or-nothing merge meant whichever
// was recorded first shut the other out -- and since the line is recorded
// first, every failure ended up with a line and no span.
func (origin Origin) fillFrom(other Origin) Origin {
	if origin.Source == "" {
		origin.Source = other.Source
	}
	if origin.Operation == "" {
		origin.Operation = other.Operation
	}
	return origin
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
	return Cause{Kind: CauseInterrupt, Interruption: Interruption{Cause: reason}}
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

// CaptureDefect records a recovered panic value together with its stack.
func CaptureDefect(value any) Defect {
	return Defect{Value: value, Stack: string(debug.Stack())}
}
