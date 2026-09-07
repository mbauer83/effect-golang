package effect

import (
	"context"

	runtimecore "github.com/mbauer83/effect-golang/effect/internal/runtime"
)

// Effect describes a suspended computation that requires R, may fail with an
// expected E, and may succeed with A.
//
// An Effect is a description, never a running computation: nothing is evaluated
// until a Runtime interprets it. Panics and context cancellation are
// represented separately in Exit as defects and interruption, so they never
// widen the typed E channel.
type Effect[R, E, A any] struct {
	node runtimecore.Node
}

// From constructs an Effect from its evaluator. The evaluator is suspended
// until the effect is interpreted.
func From[R, E, A any](eval func(context.Context, R) Exit[E, A]) Effect[R, E, A] {
	return fromRuntime(func(ctx context.Context, _ *runtimecore.State, env R) Exit[E, A] {
		return eval(ctx, env)
	})
}

// FromEither constructs an Effect whose Left is an expected typed failure and
// whose Right is success.
func FromEither[R, E, A any](eval func(context.Context, R) Either[E, A]) Effect[R, E, A] {
	return From(func(ctx context.Context, env R) Exit[E, A] {
		return eval(ctx, env).Fold(ExitFailure[E, A], ExitSuccess[E, A])
	})
}

// Succeed constructs an Effect that succeeds with value. R and E are retained
// as phantom channels so the value can be lifted directly into a known effect
// signature.
func Succeed[R, E, A any](value A) Effect[R, E, A] {
	return fromInstructions[R, E, A](&runtimecore.Succeed{Value: value})
}

// Fail constructs an Effect that fails with an expected typed error.
func Fail[R, A, E any](failure E) Effect[R, E, A] {
	return FailWithCause[R, A](FailCause(failure))
}

// FailWithCause constructs an Effect that terminates with a complete cause. It
// is how a combinator propagates a cause it chose not to handle without
// collapsing composite failures, defects or interruption.
func FailWithCause[R, A, E any](cause Cause[E]) Effect[R, E, A] {
	return fromInstructions[R, E, A](&runtimecore.Fail{Cause: cause.node})
}

// Try adapts the conventional Go (A, error) shape into a typed Effect using
// mapError to classify the external error into E.
func Try[R, E, A any](eval func(context.Context, R) (A, error), mapError func(error) E) Effect[R, E, A] {
	return From(func(ctx context.Context, env R) Exit[E, A] {
		value, err := eval(ctx, env)
		if err != nil {
			return ExitFailure[E, A](mapError(err))
		}
		return ExitSuccess[E](value)
	})
}

// Suspend defers construction of an effect until interpretation.
//
// It is how a description allocates the per-run state it needs -- a fresh
// accumulator, a rendezvous between branches, a counter -- without sharing that
// state between interpretations. create runs once per interpretation, so
// repeated and concurrent runs of the same Effect value stay independent.
func Suspend[R, E, A any](create func() Effect[R, E, A]) Effect[R, E, A] {
	return suspendRuntime(func(context.Context, *runtimecore.State, R) Effect[R, E, A] {
		return create()
	})
}

// run interprets fx to completion on the calling goroutine.
func (fx Effect[R, E, A]) run(ctx context.Context, state *runtimecore.State, env R) Exit[E, A] {
	return Exit[E, A]{erased: runtimecore.Interpret(ctx, state, env, fx.instructions())}
}

// Map transforms the success channel while preserving R and E.
func (fx Effect[R, E, A]) Map[B any](f func(A) B) Effect[R, E, B] {
	return fromInstructions[R, E, B](&runtimecore.Transform{
		Source: fx.instructions(),
		Apply:  erasedTransform(f),
	})
}

// MapError transforms expected typed failures while preserving R and A. Every
// Fail leaf of a composite cause is rewritten; defects and interruption are
// left untouched.
func (fx Effect[R, E, A]) MapError[E2 any](f func(E) E2) Effect[R, E2, A] {
	return fromInstructions[R, E2, A](&runtimecore.TransformCause{
		Source: fx.instructions(),
		Apply:  erasedFailureTransform(f),
	})
}

// ContramapEnv adapts a larger or differently shaped environment to R.
func (fx Effect[R, E, A]) ContramapEnv[R0 any](f func(R0) R) Effect[R0, E, A] {
	return fromInstructions[R0, E, A](&runtimecore.WithEnvironment{
		Source: fx.instructions(),
		Adapt:  erasedAdapter(f),
	})
}

// Provide supplies R and removes the environment requirement.
func (fx Effect[R, E, A]) Provide(env R) Effect[Unit, E, A] {
	return fx.ContramapEnv(constantEnvironment[Unit](env))
}

// constantEnvironment discards an outer environment in favour of one that is
// already available, which is how both Provide and a layer hand a built
// environment to its consumer.
func constantEnvironment[R0, R any](provided R) func(R0) R {
	return func(R0) R {
		return provided
	}
}

// WidenError retypes an infallible effect's unused failure channel so it can
// compose with effects that do fail.
//
// It is total and free. Never is uninhabited, so no Never value exists to place
// in a typed failure and an Effect[R, Never, A] can never contain one; reading
// its cause as a Cause[E] therefore cannot invent one. Defects and interruption
// are unaffected, because neither lives in E.
func WidenError[E, R, A any](fx Effect[R, Never, A]) Effect[R, E, A] {
	return fromInstructions[R, E, A](fx.instructions())
}
