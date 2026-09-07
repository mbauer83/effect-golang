package effect

import (
	"context"
	"runtime/debug"
)

// Effect describes a suspended computation that requires R, may fail with an
// expected E, and may succeed with A.
//
// Panics and context cancellation are represented separately in Exit as
// defects and interruption, so they never widen the typed E channel.
type Effect[R, E, A any] struct {
	eval func(context.Context, R) Exit[E, A]
}

// From constructs an Effect from its evaluator. The evaluator is suspended
// until Run is called.
func From[R, E, A any](eval func(context.Context, R) Exit[E, A]) Effect[R, E, A] {
	return Effect[R, E, A]{eval: eval}
}

// FromEither constructs an Effect whose Left is an expected typed failure and
// whose Right is success.
func FromEither[R, E, A any](eval func(context.Context, R) Either[E, A]) Effect[R, E, A] {
	return From(func(ctx context.Context, env R) Exit[E, A] {
		result := eval(ctx, env)
		if value, ok := result.RightValue(); ok {
			return ExitSuccess[E](value)
		}
		failure, _ := result.LeftValue()
		return ExitFailure[E, A](failure)
	})
}

// Succeed constructs an Effect that succeeds with value. R and E are retained
// as phantom channels so the value can be lifted directly into a known effect
// signature.
func Succeed[R, E, A any](value A) Effect[R, E, A] {
	return From(func(context.Context, R) Exit[E, A] {
		return ExitSuccess[E](value)
	})
}

// Fail constructs an Effect that fails with an expected typed error.
func Fail[R, A, E any](failure E) Effect[R, E, A] {
	return From(func(context.Context, R) Exit[E, A] {
		return ExitFailure[E, A](failure)
	})
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

func (fx Effect[R, E, A]) run(ctx context.Context, env R) (exit Exit[E, A]) {
	defer func() {
		if recovered := recover(); recovered != nil {
			exit = exitDefect[E, A](Defect{Value: recovered, Stack: string(debug.Stack())})
		}
	}()

	if err := ctx.Err(); err != nil {
		return exitInterrupted[E, A](err)
	}
	if fx.eval == nil {
		panic("effect: zero Effect has no evaluator")
	}
	return fx.eval(ctx, env)
}

// Map transforms the success channel while preserving R and E.
func (fx Effect[R, E, A]) Map[B any](f func(A) B) Effect[R, E, B] {
	return From(func(ctx context.Context, env R) Exit[E, B] {
		return fx.run(ctx, env).Map(f)
	})
}

// MapError transforms expected typed failures while preserving R and A.
func (fx Effect[R, E, A]) MapError[E2 any](f func(E) E2) Effect[R, E2, A] {
	return From(func(ctx context.Context, env R) Exit[E2, A] {
		return fx.run(ctx, env).MapError(f)
	})
}

// ContramapEnv adapts a larger or differently shaped environment to R.
func (fx Effect[R, E, A]) ContramapEnv[R0 any](f func(R0) R) Effect[R0, E, A] {
	return From(func(ctx context.Context, env R0) Exit[E, A] {
		return fx.run(ctx, f(env))
	})
}

// Provide supplies R and removes the environment requirement.
func (fx Effect[R, E, A]) Provide(env R) Effect[Unit, E, A] {
	return From(func(ctx context.Context, Unit) Exit[E, A] {
		return fx.run(ctx, env)
	})
}

// FlatMap is the monadic bind for a fixed R and E. Keeping the channels fixed
// avoids accumulating redundant Product[R,R] and Either[E,E] nodes.
func (fx Effect[R, E, A]) FlatMap[B any](f func(A) Effect[R, E, B]) Effect[R, E, B] {
	return From(func(ctx context.Context, env R) Exit[E, B] {
		first := fx.run(ctx, env)
		if cause, ok := first.Cause(); ok {
			return exitCause[E, B](cause)
		}
		value, _ := first.Value()
		return f(value).run(ctx, env)
	})
}

// FlatMapMerge composes effects with different R and E channels without
// widening or erasing either channel. Requirements form a Product; failures
// form an Either. The resulting structural tree is exact but not normalized.
func (fx Effect[R, E, A]) FlatMapMerge[R2, E2, B any](f func(A) Effect[R2, E2, B]) Effect[Product[R, R2], Either[E, E2], B] {
	return From(func(ctx context.Context, env Product[R, R2]) Exit[Either[E, E2], B] {
		first := fx.run(ctx, env.First).MapError(func(failure E) Either[E, E2] {
			return Left[E, E2](failure)
		})
		if cause, ok := first.Cause(); ok {
			return exitCause[Either[E, E2], B](cause)
		}

		value, _ := first.Value()
		second := f(value).run(ctx, env.Second).MapError(func(failure E2) Either[E, E2] {
			return Right[E](failure)
		})
		return second
	})
}

// Zip combines two independent effects that share R and E, evaluating left
// before right.
func (fx Effect[R, E, A]) Zip[B any](that Effect[R, E, B]) Effect[R, E, Product[A, B]] {
	return fx.FlatMap(func(left A) Effect[R, E, Product[A, B]] {
		return that.Map(func(right B) Product[A, B] {
			return ProductOf(left, right)
		})
	})
}

// ZipMerge combines independent effects with different R and E channels,
// evaluating left before right while preserving all channel information.
func (fx Effect[R, E, A]) ZipMerge[R2, E2, B any](that Effect[R2, E2, B]) Effect[Product[R, R2], Either[E, E2], Product[A, B]] {
	return fx.FlatMapMerge(func(left A) Effect[R2, E2, Product[A, B]] {
		return that.Map(func(right B) Product[A, B] {
			return ProductOf(left, right)
		})
	})
}

// CatchAll handles every expected typed failure. Defects and interruption are
// propagated unchanged into the new E2 channel.
func (fx Effect[R, E, A]) CatchAll[E2 any](handler func(E) Effect[R, E2, A]) Effect[R, E2, A] {
	return From(func(ctx context.Context, env R) Exit[E2, A] {
		exit := fx.run(ctx, env)
		if value, ok := exit.Value(); ok {
			return ExitSuccess[E2](value)
		}

		cause, _ := exit.Cause()
		if failure, ok := cause.Failure(); ok {
			return handler(failure).run(ctx, env)
		}
		return exitCause[E2, A](cause.retypeNonFailure[E2]())
	})
}
