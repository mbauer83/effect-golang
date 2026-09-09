package effect

import "time"

// Delay waits on the runtime clock before interpreting fx.
func (fx Effect[R, E, A]) Delay(duration time.Duration) Effect[R, E, A] {
	return Sleep[R, E](duration).AndThen(fx)
}

// Retry repeats fx according to policy when it returns exactly one typed Fail.
//
// Defects and interruption are never retried, and a composite cause is never
// reduced to one of its failures. When the policy is exhausted the last typed
// failure is preserved, which keeps the error channel stable.
func (fx Effect[R, E, A]) Retry[Out any](policy Schedule[E, Out]) Effect[R, E, A] {
	return retryLoop(fx, policy, exactFailure[E], preserveLastFailure[R, E, A, E, Out])
}

// RetryCause repeats fx for composite causes containing only typed failures.
// Any cause tree holding a defect or an interruption is still never retried.
func (fx Effect[R, E, A]) RetryCause[Out any](policy Schedule[Cause[E], Out]) Effect[R, E, A] {
	return retryLoop(fx, policy, allTypedFailures[E], preserveLastFailure[R, E, A, Cause[E], Out])
}

// RetryN retries at most count times after the initial execution, so
// RetryN(3) evaluates fx at most four times.
func (fx Effect[R, E, A]) RetryN(count uint64) Effect[R, E, A] {
	return fx.Retry(Recurs[E](count))
}

// RetryOrElse retries exact typed failures and evaluates fallback with the last
// failure and the final schedule output once the policy is exhausted.
func (fx Effect[R, E, A]) RetryOrElse[Out any](
	policy Schedule[E, Out],
	fallback func(E, Out) Effect[R, E, A],
) Effect[R, E, A] {
	return retryLoop(fx, policy, exactFailure[E],
		func(_ Cause[E], failure E, output Out) Effect[R, E, A] {
			return fallback(failure, output)
		},
	)
}

// exactFailure accepts only a single Fail leaf, so neither a cleanup defect nor
// one branch of a parallel failure can be silently selected.
func exactFailure[E any](cause Cause[E]) (E, bool) {
	return cause.Failure()
}

// allTypedFailures accepts a composite cause whose every leaf is a typed
// failure.
func allTypedFailures[E any](cause Cause[E]) (Cause[E], bool) {
	return cause, cause.IsFailureOnly()
}

func preserveLastFailure[R, E, A, In, Out any](cause Cause[E], _ In, _ Out) Effect[R, E, A] {
	return FailWithCause[R, A](cause)
}
