package runtime

import (
	"context"
	"github.com/mbauer83/effect-golang/internal/outcome"
)

// This file contains every point at which the interpreter calls back into
// user or public-API code. Each call is guarded so a panic becomes a defect in
// the surrounding cause instead of unwinding the interpreter and abandoning
// pending continuation frames.

func captureExitDefect(exit *outcome.Exit) {
	if recovered := recover(); recovered != nil {
		*exit = outcome.Failure(outcome.DieCause(outcome.CapturedDefect(recovered)))
	}
}

func captureDefect(defect **outcome.Defect) {
	if recovered := recover(); recovered != nil {
		captured := outcome.CapturedDefect(recovered)
		*defect = &captured
	}
}

func transformedExit(apply func(any) any, value any) (exit outcome.Exit) {
	defer captureExitDefect(&exit)
	return outcome.Success(apply(value))
}

func transformedCause(apply func(outcome.Cause) outcome.Cause, cause outcome.Cause) (exit outcome.Exit) {
	defer captureExitDefect(&exit)
	return outcome.Failure(apply(cause))
}

func evaluatedLeaf(instruction *Eval, interpretation Interpretation) (exit outcome.Exit) {
	defer captureExitDefect(&exit)
	return instruction.Run(interpretation)
}

// observedExit preserves the observed exit when a hook panics: instrumentation
// must never replace an application's result, and a cleanup defect is appended
// to the original cause rather than hiding it.
func observedExit(observe func(Interpretation, outcome.Exit) outcome.Exit, interpretation Interpretation, exit outcome.Exit) outcome.Exit {
	observed, defect := hookedExit(observe, interpretation, exit)
	if defect == nil {
		return observed
	}
	return outcome.Failure(exit.Cause().Then(outcome.DieCause(*defect)))
}

func hookedExit(
	observe func(Interpretation, outcome.Exit) outcome.Exit,
	interpretation Interpretation,
	exit outcome.Exit,
) (observed outcome.Exit, defect *outcome.Defect) {
	defer captureDefect(&defect)
	return observe(interpretation, exit), nil
}

func continuedNode(continueWith func(any) Node, value any) (node Node, defect *outcome.Defect) {
	defer captureDefect(&defect)
	return continueWith(value), nil
}

func recoveredNode(handle func(outcome.Cause) Node, cause outcome.Cause) (node Node, defect *outcome.Defect) {
	defer captureDefect(&defect)
	return handle(cause), nil
}

func suspendedNode(instruction *Suspend, interpretation Interpretation) (node Node, defect *outcome.Defect) {
	defer captureDefect(&defect)
	return instruction.Create(interpretation), nil
}

func adaptedEnvironment(adapt func(any) any, environment any) (adapted any, defect *outcome.Defect) {
	defer captureDefect(&defect)
	return adapt(environment), nil
}

func derivedState(derive func(*State) *State, state *State) (derived *State, defect *outcome.Defect) {
	defer captureDefect(&defect)
	return derive(state), nil
}

func derivedContext(
	derive func(context.Context) context.Context,
	ctx context.Context,
) (derived context.Context, defect *outcome.Defect) {
	defer captureDefect(&defect)
	return derive(ctx), nil
}
