package effect

import (
	"context"

	"github.com/mbauer83/effect-golang/capability"
	"github.com/mbauer83/effect-golang/internal/outcome"
	runtimecore "github.com/mbauer83/effect-golang/internal/runtime"
)

// retryEligibility selects the schedule input from a cause, or rejects the
// cause as one that must never be retried.
type retryEligibility[E, In any] func(Cause[E]) (In, bool)

// retryExhaustion decides what an exhausted policy produces from the last
// cause, the last schedule input and the final schedule output.
type retryExhaustion[R, E, A, In, Out any] func(Cause[E], In, Out) Effect[R, E, A]

// attemptProgress is per-interpretation mutable state. A Schedule value stays
// immutable and concurrently reusable because each interpretation allocates its
// own progress and its own driver.
type attemptProgress struct {
	number   uint64
	repeated bool
}

// retrying interprets a retry policy as ordinary sequential effect evaluation
// plus interruptible clock waits. Each attempt replaces the previous one in the
// interpreter's instruction stream instead of nesting inside it, so an
// unbounded policy adds no continuation frames and no Go stack frames.
func retrying[R, E, A, In, Out any](
	fx Effect[R, E, A],
	policy Schedule[In, Out],
	eligible retryEligibility[E, In],
	exhausted retryExhaustion[R, E, A, In, Out],
) Effect[R, E, A] {
	return suspendRuntime(func(context.Context, *runtimecore.State, R) Effect[R, E, A] {
		progress := &attemptProgress{number: 1}
		attempt := retryAttempt(fx, policy.driver(), eligible, exhausted, progress)
		return attempt.withExitObserver(reportRetryOutcome(progress))
	})
}

func retryAttempt[R, E, A, In, Out any](
	fx Effect[R, E, A],
	step scheduleStep[In, Out],
	eligible retryEligibility[E, In],
	exhausted retryExhaustion[R, E, A, In, Out],
	progress *attemptProgress,
) Effect[R, E, A] {
	return fx.CatchCause(func(cause Cause[E]) Effect[R, E, A] {
		return suspendRuntime(func(ctx context.Context, state *runtimecore.State, _ R) Effect[R, E, A] {
			input, retryable := eligible(cause)
			if !retryable {
				return FailWithCause[R, A](cause)
			}
			if reason := interruptionReason(ctx); reason != nil {
				return FailWithCause[R, A](InterruptCause[E](reason))
			}

			decision, next := step(state.Capabilities().Clock.Now(), input)
			if !decision.continueRunning {
				emitAttempt(ctx, state, retryExhaustedEvent(progress.number))
				return exhausted(cause, input, decision.output)
			}

			emitAttempt(ctx, state, retryScheduledEvent(progress.number, decision.delay))
			progress.number = nextCount(progress.number)
			progress.repeated = true
			return Sleep[R, E](decision.delay).AndThen(
				retryAttempt(fx, next, eligible, exhausted, progress),
			)
		})
	})
}

// Repeat runs fx once and then recurs according to its successful outputs. Any
// typed failure, defect or interruption stops repetition immediately.
func (fx Effect[R, E, A]) Repeat[Out any](policy Schedule[A, Out]) Effect[R, E, Out] {
	return suspendRuntime(func(context.Context, *runtimecore.State, R) Effect[R, E, Out] {
		return repeatRun(fx, policy.driver(), &attemptProgress{number: 1})
	})
}

func repeatRun[R, E, A, Out any](
	fx Effect[R, E, A],
	step scheduleStep[A, Out],
	progress *attemptProgress,
) Effect[R, E, Out] {
	return fx.FlatMap(func(value A) Effect[R, E, Out] {
		return suspendRuntime(func(ctx context.Context, state *runtimecore.State, _ R) Effect[R, E, Out] {
			decision, next := step(state.Capabilities().Clock.Now(), value)
			if !decision.continueRunning {
				emitAttempt(ctx, state, repeatCompletedEvent(progress.number))
				return Succeed[R, E](decision.output)
			}

			emitAttempt(ctx, state, repeatScheduledEvent(progress.number, decision.delay))
			progress.number = nextCount(progress.number)
			return Sleep[R, E](decision.delay).AndThen(repeatRun(fx, next, progress))
		})
	})
}

func reportRetryOutcome(progress *attemptProgress) exitObserver {
	return func(interpretation runtimecore.Interpretation, exit outcome.Exit) outcome.Exit {
		if exit.Succeeded() && progress.repeated {
			emitAttempt(interpretation.Context, interpretation.State, retrySucceededEvent(progress.number))
		}
		return exit
	}
}

func emitAttempt(ctx context.Context, state *runtimecore.State, describe attemptEvent) {
	if !state.Observing() {
		return
	}
	state.Emit(ctx, describe(state))
}

// attemptEvent defers event construction until the runtime confirms an observer
// is installed, so a disabled category costs one branch.
type attemptEvent func(*runtimecore.State) capability.RuntimeEvent
