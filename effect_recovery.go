package effect

import (
	"github.com/mbauer83/effect-golang/internal/outcome"
	runtimecore "github.com/mbauer83/effect-golang/internal/runtime"
)

// CatchCause handles the complete cause, including composite failures, defects
// and interruption. It is intentionally more powerful than CatchAll; recovering
// from interruption in particular defeats structured cancellation unless the
// handler re-raises it.
func (fx Effect[R, E, A]) CatchCause[E2 any](handler func(Cause[E]) Effect[R, E2, A]) Effect[R, E2, A] {
	return fromInstructions[R, E2, A](&runtimecore.Recover{
		Source: fx.instructions(),
		Handle: erasedRecovery(handler),
	})
}

// CatchAll handles a leaf typed failure while keeping the error channel fixed.
// Composite causes, defects, and interruption are propagated unchanged.
func (fx Effect[R, E, A]) CatchAll(handler func(E) Effect[R, E, A]) Effect[R, E, A] {
	return fx.CatchCause(func(cause Cause[E]) Effect[R, E, A] {
		failure, ok := cause.Failure()
		if !ok {
			return FailWithCause[R, A](cause)
		}
		return handler(failure)
	})
}

// CatchAllMerge handles a leaf typed failure with a different error type.
// Unhandled original failures are tagged Left; handler failures are tagged Right.
func CatchAllMerge[R, E, A, E2 any](fx Effect[R, E, A], handler func(E) Effect[R, E2, A]) Effect[R, Either[E, E2], A] {
	return fx.MapError(Left[E, E2]).CatchCause(
		func(cause Cause[Either[E, E2]]) Effect[R, Either[E, E2], A] {
			original, handled := originalFailure(cause)
			if !handled {
				return FailWithCause[R, A](cause)
			}
			return handler(original).MapError(Right[E, E2])
		},
	)
}

// originalFailure reports the pre-tagging failure only for an exact Fail leaf
// that CatchAllMerge itself tagged as Left.
func originalFailure[E, E2 any](cause Cause[Either[E, E2]]) (E, bool) {
	tagged, isLeaf := cause.Failure()
	if !isLeaf {
		var missing E
		return missing, false
	}
	return tagged.LeftValue()
}

// ExitOf reifies fx's complete outcome as a successful value, so a caller can
// inspect typed failure, defect and interruption without leaving the effect
// world. It observes the exit rather than recovering from it, which is why an
// interruption remains visible instead of re-interrupting the observer.
//
// It is a package function because its success channel is constructed from the
// effect's own channels (golang/go#80172).
func ExitOf[R, E, A any](fx Effect[R, E, A]) Effect[R, Never, Exit[E, A]] {
	return fromInstructions[R, Never, Exit[E, A]](&runtimecore.OnExit{
		Source:  fx.instructions(),
		Observe: reifyExit[E, A],
	})
}

func reifyExit[E, A any](_ runtimecore.Interpretation, exit outcome.Exit) outcome.Exit {
	return outcome.Success(Exit[E, A]{erased: exit})
}

// Fold eliminates both of an effect's failure and success channels into one
// success value. A panic raised by either handler becomes a defect of the
// resulting effect rather than being folded back into it.
//
// Like ExitOf it is a package function: its implementation instantiates an
// effect over this effect's own channels (golang/go#80172).
func Fold[R, E, A, B any](
	fx Effect[R, E, A],
	failure func(Cause[E]) B,
	success func(A) B,
) Effect[R, Never, B] {
	return ExitOf(fx).Map(func(exit Exit[E, A]) B {
		return exit.Fold(failure, success)
	})
}

// FailuresAsDefects rewrites every typed failure in fx as a defect, leaving its
// success channel untouched and its cause structure intact.
//
// It is how a workflow whose failure channel must be Never -- a scope
// finalizer or an Ensuring block -- keeps a real failure visible instead of
// discarding it. Section 9.2 of the runtime design lists the three honest
// choices for a release error: absorb it deliberately, convert it to a defect
// with this operation, or handle it before registration so it can still
// participate in E. Silently dropping it is not one of them.
func FailuresAsDefects[R, E, A any](fx Effect[R, E, A]) Effect[R, Never, A] {
	return fromInstructions[R, Never, A](&runtimecore.TransformCause{
		Source: fx.instructions(),
		Apply:  outcome.FailuresAsDefects,
	})
}
