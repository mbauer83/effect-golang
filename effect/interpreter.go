package effect

import (
	"context"
	"fmt"

	runtimecore "github.com/mbauer83/effect-golang/effect/internal/runtime"
)

// Interpreter evaluates effects inside one running interpretation.
//
// It is the seam for combinators this package does not provide. Everything an
// effect needs in order to behave correctly is ambient: the runtime's
// capabilities, the current scope, the observation metadata and the
// cancellation. A combinator that reached for the package-level Run instead
// would silently get a fresh runtime with live defaults, a scope of its own and
// no cancellation -- so a Sleep would ignore a test clock and an acquisition
// would register in a lifetime nobody closes.
//
// It is valid only while the body that received it is running. Using it
// afterwards reports a defect rather than evaluating against a lifetime that has
// already ended.
type Interpreter[R, E any] struct {
	ctx         context.Context
	state       *runtimecore.State
	environment R
	validity    *interpreterValidity
}

type interpreterValidity struct {
	live bool
}

// WithInterpreter runs body with an Interpreter for the current interpretation.
//
// The Interpreter stops working when body returns, so body must not publish it.
// A combinator that needs to evaluate effects later should hold the Effect
// values and interpret them inside its own WithInterpreter call.
func WithInterpreter[R, E, A any](body func(Interpreter[R, E]) Exit[E, A]) Effect[R, E, A] {
	return fromRuntime(func(ctx context.Context, state *runtimecore.State, env R) Exit[E, A] {
		validity := &interpreterValidity{live: true}
		defer func() { validity.live = false }()
		return body(Interpreter[R, E]{
			ctx:         ctx,
			state:       state,
			environment: env,
			validity:    validity,
		})
	})
}

// Interpret interprets fx in the current interpretation and returns its complete
// outcome, so the caller decides what a failure, a defect or an interruption
// means.
//
// It is a package function because its result is built from the interpreter's
// own type arguments (golang/go#80172).
func Interpret[R, E, A any](interpreter Interpreter[R, E], fx Effect[R, E, A]) Exit[E, A] {
	if interpreter.validity == nil || !interpreter.validity.live {
		panic(fmt.Errorf("effect: Interpreter used after its WithInterpreter body returned"))
	}
	return fx.run(interpreter.ctx, interpreter.state, interpreter.environment)
}

// Context returns the context this interpretation is running under, for a
// combinator that needs to select on cancellation itself.
func (interpreter Interpreter[R, E]) Context() context.Context {
	return interpreter.ctx
}
