package effect

import "context"

// Operations carries an effect program's R and E channels so standard
// capability constructors inherit them without repeated type arguments.
// It contains no runtime state and its zero value is usable.
type Operations[R, E any] struct{}

// For selects the requirement and expected-error channels once for a group of
// standard effect constructors.
func For[R, E any]() Operations[R, E] {
	return Operations[R, E]{}
}

// From suspends an evaluator using these channels.
func (Operations[R, E]) From[A any](eval func(context.Context, R) Exit[E, A]) Effect[R, E, A] {
	return From(eval)
}

// FromEither suspends an Either-returning evaluator using these channels.
func (Operations[R, E]) FromEither[A any](eval func(context.Context, R) Either[E, A]) Effect[R, E, A] {
	return FromEither(eval)
}

// Succeed lifts a value into these channels.
func (Operations[R, E]) Succeed[A any](value A) Effect[R, E, A] {
	return Succeed[R, E](value)
}

// Fail constructs a typed failure in these channels. A remains explicit
// because a failure value provides no successful value from which Go can infer it.
func (Operations[R, E]) Fail[A any](failure E) Effect[R, E, A] {
	return Fail[R, A](failure)
}

// Try adapts a conventional Go evaluator using these channels.
func (Operations[R, E]) Try[A any](
	eval func(context.Context, R) (A, error),
	mapError func(error) E,
) Effect[R, E, A] {
	return Try(eval, mapError)
}

// Do starts a typed workflow using these channels.
func (Operations[R, E]) Do[S any](factory func() S) Workflow[R, E, S] {
	return Do[R, E](factory)
}

// WidenError retypes an infallible effect into these channels, which is how a
// fiber observation or a cleanup workflow composes with failing work.
func (Operations[R, E]) WidenError[A any](fx Effect[R, Never, A]) Effect[R, E, A] {
	return WidenError[E](fx)
}

// Suspend defers construction until interpretation using these channels, so a
// description can allocate its own per-run state.
func (Operations[R, E]) Suspend[A any](create func() Effect[R, E, A]) Effect[R, E, A] {
	return Suspend(create)
}

// FailuresAsDefects rewrites an effect's typed failures as defects so it can be
// used where the failure channel must be Never, such as a release workflow.
func (Operations[R, E]) FailuresAsDefects[A any](fx Effect[R, E, A]) Effect[R, Never, A] {
	return FailuresAsDefects(fx)
}

// CheckInterrupt inserts a cooperative cancellation checkpoint using these
// channels.
func (Operations[R, E]) CheckInterrupt() Effect[R, E, Unit] {
	return CheckInterrupt[R, E]()
}
