package runtime

import (
	"context"
	"github.com/mbauer83/effect-golang/effect/internal/outcome"
)

// This file contains every point at which the interpreter calls back into
// user or public-API code. Each call is guarded so a panic becomes a defect in
// the surrounding cause instead of unwinding the interpreter and abandoning
// pending continuation frames.

func captureExitDefect(exit *outcome.Exit) {
	if recovered := recover(); recovered != nil {
		*exit = outcome.Failure(outcome.DieCause(outcome.CaptureDefect(recovered)))
	}
}

func captureDefect(defect **outcome.Defect) {
	if recovered := recover(); recovered != nil {
		captured := outcome.CaptureDefect(recovered)
		*defect = &captured
	}
}

func transformExit(apply func(any) any, value any) (exit outcome.Exit) {
	defer captureExitDefect(&exit)
	return outcome.Success(apply(value))
}

func transformCause(apply func(outcome.Cause) outcome.Cause, cause outcome.Cause) (exit outcome.Exit) {
	defer captureExitDefect(&exit)
	return outcome.Failure(apply(cause))
}

func evalLeaf(instruction *Eval, interpretation Interpretation) (exit outcome.Exit) {
	defer captureExitDefect(&exit)
	return instruction.Run(interpretation)
}

// observeExit preserves the observed exit when a hook panics: instrumentation
// must never replace an application's result, and a cleanup defect is appended
// to the original cause rather than hiding it.
func observeExit(observe func(Interpretation, outcome.Exit) outcome.Exit, interpretation Interpretation, exit outcome.Exit) outcome.Exit {
	result, defect := runHook(observe, interpretation, exit)
	if defect == nil {
		return result
	}
	return outcome.Failure(exit.Cause().Then(outcome.DieCause(*defect)))
}

func runHook(
	observe func(Interpretation, outcome.Exit) outcome.Exit,
	interpretation Interpretation,
	exit outcome.Exit,
) (result outcome.Exit, defect *outcome.Defect) {
	defer captureDefect(&defect)
	return observe(interpretation, exit), nil
}

func continueNode(continueWith func(any) Node, value any) (node Node, defect *outcome.Defect) {
	defer captureDefect(&defect)
	return continueWith(value), nil
}

func handleCause(handle func(outcome.Cause) Node, cause outcome.Cause) (node Node, defect *outcome.Defect) {
	defer captureDefect(&defect)
	return handle(cause), nil
}

func createNode(instruction *Suspend, interpretation Interpretation) (node Node, defect *outcome.Defect) {
	defer captureDefect(&defect)
	return instruction.Create(interpretation), nil
}

func adaptEnvironment(adapt func(any) any, environment any) (result any, defect *outcome.Defect) {
	defer captureDefect(&defect)
	return adapt(environment), nil
}

func deriveState(derive func(*State) *State, state *State) (result *State, defect *outcome.Defect) {
	defer captureDefect(&defect)
	return derive(state), nil
}

func deriveContext(
	derive func(context.Context) context.Context,
	ctx context.Context,
) (result context.Context, defect *outcome.Defect) {
	defer captureDefect(&defect)
	return derive(ctx), nil
}
