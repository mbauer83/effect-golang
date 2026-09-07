package runtime

import (
	"context"
	"runtime/debug"
)

// This file contains every point at which the interpreter calls back into
// user or public-API code. Each call is guarded so a panic becomes a defect in
// the surrounding cause instead of unwinding the interpreter and abandoning
// pending continuation frames.

// CapturedDefect records a recovered panic value together with its stack.
func CapturedDefect(recovered any) Defect {
	return Defect{Value: recovered, Stack: string(debug.Stack())}
}

func captureExitDefect(exit *Exit) {
	if recovered := recover(); recovered != nil {
		*exit = Failure(DieCause(CapturedDefect(recovered)))
	}
}

func captureDefect(defect **Defect) {
	if recovered := recover(); recovered != nil {
		captured := CapturedDefect(recovered)
		*defect = &captured
	}
}

func transformedExit(apply func(any) any, value any) (exit Exit) {
	defer captureExitDefect(&exit)
	return Success(apply(value))
}

func transformedCause(apply func(Cause) Cause, cause Cause) (exit Exit) {
	defer captureExitDefect(&exit)
	return Failure(apply(cause))
}

func evaluatedLeaf(instruction *Eval, interpretation Interpretation) (exit Exit) {
	defer captureExitDefect(&exit)
	return instruction.Run(interpretation)
}

// observedExit preserves the observed exit when a hook panics: instrumentation
// must never replace an application's result, and a cleanup defect is appended
// to the original cause rather than hiding it.
func observedExit(observe func(Interpretation, Exit) Exit, interpretation Interpretation, exit Exit) Exit {
	observed, defect := hookedExit(observe, interpretation, exit)
	if defect == nil {
		return observed
	}
	return Failure(exit.Cause().Then(DieCause(*defect)))
}

func hookedExit(
	observe func(Interpretation, Exit) Exit,
	interpretation Interpretation,
	exit Exit,
) (observed Exit, defect *Defect) {
	defer captureDefect(&defect)
	return observe(interpretation, exit), nil
}

func continuedNode(continueWith func(any) Node, value any) (node Node, defect *Defect) {
	defer captureDefect(&defect)
	return continueWith(value), nil
}

func recoveredNode(handle func(Cause) Node, cause Cause) (node Node, defect *Defect) {
	defer captureDefect(&defect)
	return handle(cause), nil
}

func suspendedNode(instruction *Suspend, interpretation Interpretation) (node Node, defect *Defect) {
	defer captureDefect(&defect)
	return instruction.Create(interpretation), nil
}

func adaptedEnvironment(adapt func(any) any, environment any) (adapted any, defect *Defect) {
	defer captureDefect(&defect)
	return adapt(environment), nil
}

func derivedState(derive func(*State) *State, state *State) (derived *State, defect *Defect) {
	defer captureDefect(&defect)
	return derive(state), nil
}

func derivedContext(
	derive func(context.Context) context.Context,
	ctx context.Context,
) (derived context.Context, defect *Defect) {
	defer captureDefect(&defect)
	return derive(ctx), nil
}
