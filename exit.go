package effect

// Exit is the complete result of running an Effect: either a Cause[E] or a
// successful A. Defects and interruption therefore do not widen E.
type Exit[E, A any] struct {
	result Either[Cause[E], A]
}

// ExitSuccess constructs a successful Exit.
func ExitSuccess[E, A any](value A) Exit[E, A] {
	return Exit[E, A]{result: Right[Cause[E]](value)}
}

// ExitFailure constructs an Exit with an expected typed failure.
func ExitFailure[E, A any](failure E) Exit[E, A] {
	return Exit[E, A]{result: Left[Cause[E], A](failureCause(failure))}
}

func exitDefect[E, A any](defect Defect) Exit[E, A] {
	return Exit[E, A]{result: Left[Cause[E], A](defectCause[E](defect))}
}

func exitInterrupted[E, A any](err error) Exit[E, A] {
	return Exit[E, A]{result: Left[Cause[E], A](interruptedCause[E](err))}
}

func exitCause[E, A any](cause Cause[E]) Exit[E, A] {
	return Exit[E, A]{result: Left[Cause[E], A](cause)}
}

// IsSuccess reports whether the effect succeeded.
func (x Exit[E, A]) IsSuccess() bool {
	return x.result.IsRight()
}

// IsFailure reports whether the effect terminated with any Cause.
func (x Exit[E, A]) IsFailure() bool {
	return x.result.IsLeft()
}

// Value returns the success value and true when the Exit succeeded.
func (x Exit[E, A]) Value() (A, bool) {
	return x.result.RightValue()
}

// Cause returns the cause and true when the Exit failed.
func (x Exit[E, A]) Cause() (Cause[E], bool) {
	return x.result.LeftValue()
}

// Fold eliminates Exit by handling failure and success.
func (x Exit[E, A]) Fold[T any](failure func(Cause[E]) T, success func(A) T) T {
	return x.result.Fold(failure, success)
}

// Map transforms a successful value.
func (x Exit[E, A]) Map[B any](f func(A) B) Exit[E, B] {
	if value, ok := x.Value(); ok {
		return ExitSuccess[E](f(value))
	}
	cause, _ := x.Cause()
	return exitCause[E, B](cause)
}

// MapError transforms only expected typed failures.
func (x Exit[E, A]) MapError[E2 any](f func(E) E2) Exit[E2, A] {
	if value, ok := x.Value(); ok {
		return ExitSuccess[E2](value)
	}
	cause, _ := x.Cause()
	return exitCause[E2, A](cause.MapFailure(f))
}
