package effect

import (
	"context"

	"github.com/mbauer83/effect-golang/effect/internal/outcome"
	runtimecore "github.com/mbauer83/effect-golang/effect/internal/runtime"
)

// Ensuring runs finalize after fx in every outcome -- success, typed failure,
// defect and interruption -- and preserves both causes.
//
// finalize runs with a context detached from cancellation, so an already
// canceled caller cannot skip cleanup. Its failure channel is Never for the
// same reason a scope finalizer's is: cleanup must not widen E. A defect it
// raises is appended to fx's cause with Then.
func (fx Effect[R, E, A]) Ensuring(finalize Effect[R, Never, Unit]) Effect[R, E, A] {
	return fx.OnExit(func(Exit[E, A]) Effect[R, Never, Unit] {
		return finalize
	})
}

// OnExit is Ensuring with access to the outcome being finalized, which lets
// cleanup distinguish committing from rolling back.
func (fx Effect[R, E, A]) OnExit(finalize func(Exit[E, A]) Effect[R, Never, Unit]) Effect[R, E, A] {
	return fx.withExitObserver(func(
		interpretation runtimecore.Interpretation,
		exit outcome.Exit,
	) outcome.Exit {
		cleanup := finalizeExit(interpretation, finalize, Exit[E, A]{erased: exit})
		if cleanup.IsEmpty() {
			return exit
		}
		return outcome.Failure(exit.Cause().Then(cleanup))
	})
}

func finalizeExit[R, E, A any](
	interpretation runtimecore.Interpretation,
	finalize func(Exit[E, A]) Effect[R, Never, Unit],
	exit Exit[E, A],
) outcome.Cause {
	environment := asEnvironment[R](interpretation.Environment)
	cleanup := context.WithoutCancel(interpretation.Context)
	result := finalize(exit).run(cleanup, interpretation.State, environment)
	cause, _ := result.Cause()
	return cause.node
}
