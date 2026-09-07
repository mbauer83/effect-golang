package effect

import (
	"time"

	runtimecore "github.com/mbauer83/effect-golang/internal/runtime"
)

// Timeout races fx against the runtime clock and reports whether it completed.
//
// A Left result means the duration elapsed first; a Right result carries fx's
// value. The timed-out work is canceled with a distinct timeout reason and
// awaited, including its resource finalizers, before Timeout completes -- it
// never returns while the abandoned work is still executing.
//
// Waiting uses the runtime clock, so a test clock drives it without any
// wall-clock sleep. It is a package function because its success channel is
// built from fx's own channels (golang/go#80172).
func Timeout[R, E, A any](fx Effect[R, E, A], duration time.Duration) Effect[R, E, Either[Unit, A]] {
	return timing(
		fx.Map(Right[Unit, A]),
		duration,
		ExitSuccess[E](Left[Unit, A](Unit{})),
	)
}

// TimeoutFail races fx against the runtime clock and fails with onTimeout when
// the duration elapses first, which keeps the timeout in the caller's own
// error vocabulary instead of smuggling a generic timeout error into every E.
func (fx Effect[R, E, A]) TimeoutFail(duration time.Duration, onTimeout E) Effect[R, E, A] {
	return timing(fx, duration, ExitFailure[E, A](onTimeout))
}

// TimeoutTo races fx against the runtime clock and substitutes fallback when
// the duration elapses first.
func (fx Effect[R, E, A]) TimeoutTo(duration time.Duration, fallback A) Effect[R, E, A] {
	return timing(fx, duration, ExitSuccess[E](fallback))
}

func timing[R, E, A any](fx Effect[R, E, A], duration time.Duration, elapsed Exit[E, A]) Effect[R, E, A] {
	return pairing(
		fx,
		Sleep[R, E](duration),
		settleAlways,
		runtimecore.ErrTimedOut,
		timedResult(elapsed),
	)
}

// timedResult resolves a timed race. When the clock wins, anything the
// abandoned work reported beyond the interruption this timeout induced -- a
// finalizer defect, for instance -- is still composed into the result, because
// a timeout must not hide a cleanup failure.
func timedResult[E, A any](elapsed Exit[E, A]) pairResolver[E, A] {
	return func(outcome runtimecore.PairOutcome, induced error) Exit[E, A] {
		clockWon := outcome.First == runtimecore.RightSide && outcome.Right.Succeeded()
		if !clockWon {
			return Exit[E, A]{erased: outcome.Left}
		}

		abandoned := outcome.Left.Cause()
		if abandoned.IsEmpty() || runtimecore.WasInduced(abandoned, induced) {
			return elapsed
		}
		return Exit[E, A]{erased: runtimecore.Failure(elapsed.erased.Cause().Then(abandoned))}
	}
}
