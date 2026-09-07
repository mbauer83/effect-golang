package effect

import "context"

// Workflow is a visually flat sequence of dependent effects over one explicit
// caller-defined state. It is a convenience over FlatMap, not a runtime mode.
type Workflow[R, E, S any] struct {
	state Effect[R, E, S]
}

// Do starts a workflow. factory is evaluated once for each interpretation, so
// retries and concurrent runs never share workflow state by accident.
func Do[R, E, S any](factory func() S) Workflow[R, E, S] {
	return Workflow[R, E, S]{
		state: From(func(context.Context, R) Exit[E, S] {
			return ExitSuccess[E](factory())
		}),
	}
}

// Bind evaluates one dependent step and incorporates its result into the next
// state. update should return a new state value instead of mutating shared data.
func (workflow Workflow[R, E, S]) Bind[A any](
	step func(S) Effect[R, E, A],
	update func(S, A) S,
) Workflow[R, E, S] {
	return Workflow[R, E, S]{
		state: workflow.state.FlatMap(func(state S) Effect[R, E, S] {
			return step(state).Map(func(value A) S {
				return update(state, value)
			})
		}),
	}
}

// Yield completes a workflow by deriving its successful result from the final
// state.
func (workflow Workflow[R, E, S]) Yield[A any](result func(S) A) Effect[R, E, A] {
	return workflow.state.Map(result)
}
