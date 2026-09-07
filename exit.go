package effect

import (
	"fmt"

	"github.com/mbauer83/effect-golang/internal/outcome"
)

// Exit is the complete result of running an Effect: either a Cause[E] or a
// successful A. Defects and interruption therefore do not widen E.
//
// Its zero value is an unsuccessful termination with the empty cause.
type Exit[E, A any] struct {
	erased outcome.Exit
}

// ExitSuccess constructs a successful Exit.
func ExitSuccess[E, A any](value A) Exit[E, A] {
	return Exit[E, A]{erased: outcome.Success(value)}
}

// ExitFailure constructs an Exit with an expected typed failure.
func ExitFailure[E, A any](failure E) Exit[E, A] {
	return ExitCause[E, A](FailCause(failure))
}

// ExitCause constructs an Exit with a complete failure cause.
func ExitCause[E, A any](cause Cause[E]) Exit[E, A] {
	return Exit[E, A]{erased: outcome.Failure(cause.node)}
}

func exitInterrupted[E, A any](reason error) Exit[E, A] {
	return ExitCause[E, A](InterruptCause[E](reason))
}

// IsSuccess reports whether the effect succeeded.
func (x Exit[E, A]) IsSuccess() bool {
	return x.erased.Succeeded()
}

// IsFailure reports whether the effect terminated with any Cause.
func (x Exit[E, A]) IsFailure() bool {
	return !x.erased.Succeeded()
}

// Value returns the success value and true when the Exit succeeded.
func (x Exit[E, A]) Value() (A, bool) {
	if !x.erased.Succeeded() {
		var missing A
		return missing, false
	}
	return typedValue[A](x.erased.Value()), true
}

// Cause returns the cause and true when the Exit failed.
func (x Exit[E, A]) Cause() (Cause[E], bool) {
	if x.erased.Succeeded() {
		return Cause[E]{}, false
	}
	return Cause[E]{node: x.erased.Cause()}, true
}

// Fold eliminates Exit by handling failure and success.
func (x Exit[E, A]) Fold[T any](failure func(Cause[E]) T, success func(A) T) T {
	if value, ok := x.Value(); ok {
		return success(value)
	}
	cause, _ := x.Cause()
	return failure(cause)
}

// Map transforms a successful value.
func (x Exit[E, A]) Map[B any](f func(A) B) Exit[E, B] {
	if value, ok := x.Value(); ok {
		return ExitSuccess[E](f(value))
	}
	cause, _ := x.Cause()
	return ExitCause[E, B](cause)
}

// MapError transforms only expected typed failures.
func (x Exit[E, A]) MapError[E2 any](f func(E) E2) Exit[E2, A] {
	if value, ok := x.Value(); ok {
		return ExitSuccess[E2](value)
	}
	cause, _ := x.Cause()
	return ExitCause[E2, A](cause.MapFailure(f))
}

// String renders a successful value or the complete cause tree.
func (x Exit[E, A]) String() string {
	return x.Fold(
		func(cause Cause[E]) string { return "Failure(" + cause.String() + ")" },
		func(value A) string { return fmt.Sprintf("Success(%v)", value) },
	)
}
