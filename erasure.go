package effect

import (
	"context"

	runtimecore "github.com/mbauer83/effect-golang/internal/runtime"
)

// This file is the only place where the public typed model meets the runtime's
// erased representation.
//
// Go cannot express a heterogeneous continuation stack, so the interpreter
// carries environments, success values and typed failures as any. Every erased
// value is written and read exclusively by the helpers below, each of which is
// reached only from an Effect[R, E, A] whose channel already fixes the dynamic
// type. The assertions are therefore guaranteed by construction rather than by
// a runtime check, and no top type appears in the public API.

// typedEnvironment recovers the R channel of the effect being interpreted.
func typedEnvironment[R any](environment any) R {
	return recoverErased[R](environment)
}

// typedValue recovers the A channel of the effect that produced a value.
func typedValue[A any](value any) A {
	return recoverErased[A](value)
}

// typedFailure recovers the E channel of the effect that produced a cause.
func typedFailure[E any](failure any) E {
	return recoverErased[E](failure)
}

// recoverErased uses the two-result assertion so a nil erased value belonging
// to an interface-typed channel yields that channel's zero value instead of
// panicking. No other mismatch is reachable from the typed constructors.
func recoverErased[T any](value any) T {
	typed, _ := value.(T)
	return typed
}

// The lifters below are the complete set of places where a typed callback
// becomes an erased one. Keeping them together is what makes the boundary
// checkable rather than merely claimed: no file outside this one and the
// internal runtime package needs to mention an erased value at all.

// erasedTransform lifts a typed success transform.
func erasedTransform[A, B any](transform func(A) B) func(any) any {
	return func(value any) any {
		return transform(typedValue[A](value))
	}
}

// erasedContinuation lifts a typed monadic continuation.
func erasedContinuation[R, E, A, B any](
	continueWith func(A) Effect[R, E, B],
) func(any) runtimecore.Node {
	return func(value any) runtimecore.Node {
		return continueWith(typedValue[A](value)).instructions()
	}
}

// erasedAdapter lifts a typed environment adapter.
func erasedAdapter[R0, R any](adapt func(R0) R) func(any) any {
	return func(environment any) any {
		return adapt(typedEnvironment[R0](environment))
	}
}

// erasedFailureTransform lifts a typed failure transform over every Fail leaf,
// leaving defects and interruption untouched.
func erasedFailureTransform[E, E2 any](transform func(E) E2) func(runtimecore.Cause) runtimecore.Cause {
	return func(cause runtimecore.Cause) runtimecore.Cause {
		return runtimecore.MapCauseFailure(cause, func(failure any) any {
			return transform(typedFailure[E](failure))
		})
	}
}

// erasedFolder lifts a typed cause folder, so the stack-safe traversal can live
// once in the runtime while elimination stays typed.
func erasedFolder[E, A any](folder CauseFolder[E, A]) runtimecore.CauseFolder[A] {
	return runtimecore.CauseFolder[A]{
		Empty: folder.Empty,
		Failure: func(failure any) A {
			return folder.Failure(typedFailure[E](failure))
		},
		Defect:       folder.Defect,
		Interruption: folder.Interruption,
		Then:         folder.Then,
		Both:         folder.Both,
	}
}

// erasedRecovery lifts a typed cause handler.
func erasedRecovery[R, E, E2, A any](
	handler func(Cause[E]) Effect[R, E2, A],
) func(runtimecore.Cause) runtimecore.Node {
	return func(cause runtimecore.Cause) runtimecore.Node {
		return handler(Cause[E]{node: cause}).instructions()
	}
}

// instructions returns the erased program for fx. A zero Effect has none, so it
// reports a defect during interpretation rather than panicking at construction:
// building an effect must never execute or fail.
func (fx Effect[R, E, A]) instructions() runtimecore.Node {
	if fx.node == nil {
		return &runtimecore.Eval{Run: missingInstructions}
	}
	return fx.node
}

func missingInstructions(runtimecore.Interpretation) runtimecore.Exit {
	panic("effect: zero Effect has no instructions")
}

// fromInstructions wraps an erased program in its typed channels.
func fromInstructions[R, E, A any](node runtimecore.Node) Effect[R, E, A] {
	return Effect[R, E, A]{node: node}
}

// fromRuntime lifts a runtime-aware evaluator into a leaf instruction. Base
// capability effects use it to reach the runtime's clock, filesystem, logger
// and observer without those services entering the R channel.
func fromRuntime[R, E, A any](eval func(context.Context, *runtimecore.State, R) Exit[E, A]) Effect[R, E, A] {
	return fromInstructions[R, E, A](&runtimecore.Eval{
		Run: func(interpretation runtimecore.Interpretation) runtimecore.Exit {
			environment := typedEnvironment[R](interpretation.Environment)
			return eval(interpretation.Context, interpretation.State, environment).erased
		},
	})
}

// suspendRuntime defers instruction construction to interpretation time, so a
// combinator may read the context, runtime state or environment before
// deciding which program to run next.
func suspendRuntime[R, E, A any](
	create func(context.Context, *runtimecore.State, R) Effect[R, E, A],
) Effect[R, E, A] {
	return fromInstructions[R, E, A](&runtimecore.Suspend{
		Create: func(interpretation runtimecore.Interpretation) runtimecore.Node {
			environment := typedEnvironment[R](interpretation.Environment)
			return create(interpretation.Context, interpretation.State, environment).instructions()
		},
	})
}
