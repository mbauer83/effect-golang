package effect

import runtimecore "github.com/mbauer83/effect-golang/internal/runtime"

// FlatMap is the monadic bind for a fixed R and E. Keeping the channels fixed
// avoids accumulating redundant Product[R,R] and Either[E,E] nodes.
func (fx Effect[R, E, A]) FlatMap[B any](f func(A) Effect[R, E, B]) Effect[R, E, B] {
	return fromInstructions[R, E, B](&runtimecore.Bind{
		Source:   fx.instructions(),
		Continue: erasedContinuation(f),
	})
}

// As replaces a successful value with value.
func (fx Effect[R, E, A]) As[B any](value B) Effect[R, E, B] {
	return fx.Map(func(A) B {
		return value
	})
}

// Tap runs inspect after a success and preserves the original value.
func (fx Effect[R, E, A]) Tap[B any](inspect func(A) Effect[R, E, B]) Effect[R, E, A] {
	return fx.FlatMap(func(value A) Effect[R, E, A] {
		return inspect(value).As(value)
	})
}

// AndThen runs that after fx succeeds and returns that's value.
func (fx Effect[R, E, A]) AndThen[B any](that Effect[R, E, B]) Effect[R, E, B] {
	return fx.FlatMap(func(A) Effect[R, E, B] {
		return that
	})
}

// Flatten removes one nested Effect layer. It is a package function because
// Go receiver declarations cannot specialize Effect's success parameter.
func Flatten[R, E, A any](fx Effect[R, E, Effect[R, E, A]]) Effect[R, E, A] {
	return fx.FlatMap(func(inner Effect[R, E, A]) Effect[R, E, A] {
		return inner
	})
}

// CheckInterrupt is a cooperative cancellation checkpoint for CPU-heavy or
// hand-written loops. The interpreter checks interruption at every instruction
// that settles a value, so this is simply the smallest such instruction that
// can be inserted into a sequence.
func CheckInterrupt[R, E any]() Effect[R, E, Unit] {
	return Succeed[R, E](Unit{})
}
