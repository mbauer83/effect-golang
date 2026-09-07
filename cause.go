package effect

// CauseKind identifies why an effect terminated unsuccessfully.
type CauseKind uint8

const (
	// CauseFailure is an expected, typed failure in the E channel.
	CauseFailure CauseKind = iota
	// CauseDefect is an unexpected panic or explicitly raised defect.
	CauseDefect
	// CauseInterrupted is cooperative interruption, normally via context cancellation.
	CauseInterrupted
)

// Defect records an unexpected panic value and its stack at the capture point.
type Defect struct {
	Value any
	Stack string
}

// Cause keeps expected failures separate from defects and interruption.
type Cause[E any] struct {
	kind         CauseKind
	failure      E
	defect       Defect
	interruption error
}

func failureCause[E any](failure E) Cause[E] {
	return Cause[E]{kind: CauseFailure, failure: failure}
}

func defectCause[E any](defect Defect) Cause[E] {
	return Cause[E]{kind: CauseDefect, defect: defect}
}

func interruptedCause[E any](err error) Cause[E] {
	return Cause[E]{kind: CauseInterrupted, interruption: err}
}

// Kind returns the category of the cause.
func (c Cause[E]) Kind() CauseKind {
	return c.kind
}

// Failure returns the typed failure when Kind is CauseFailure.
func (c Cause[E]) Failure() (E, bool) {
	return c.failure, c.kind == CauseFailure
}

// Defect returns the defect when Kind is CauseDefect.
func (c Cause[E]) Defect() (Defect, bool) {
	return c.defect, c.kind == CauseDefect
}

// Interruption returns the interruption error when Kind is CauseInterrupted.
func (c Cause[E]) Interruption() (error, bool) {
	return c.interruption, c.kind == CauseInterrupted
}

// MapFailure transforms only the typed-failure channel.
func (c Cause[E]) MapFailure[E2 any](f func(E) E2) Cause[E2] {
	switch c.kind {
	case CauseFailure:
		return failureCause(f(c.failure))
	case CauseDefect:
		return defectCause[E2](c.defect)
	case CauseInterrupted:
		return interruptedCause[E2](c.interruption)
	default:
		panic("effect: unknown CauseKind")
	}
}

func (c Cause[E]) retypeNonFailure[E2 any]() Cause[E2] {
	switch c.kind {
	case CauseDefect:
		return defectCause[E2](c.defect)
	case CauseInterrupted:
		return interruptedCause[E2](c.interruption)
	default:
		panic("effect: cannot retype a typed failure without a mapping")
	}
}
