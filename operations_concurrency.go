package effect

// Fork, fiber observation and interruption produce effects whose requirement
// channel is unused and whose failure channel is either Never or the fiber's
// own E. Selecting those channels once through Operations keeps a concurrent
// program composable with FlatMap, instead of forcing a widening or an
// environment adapter at every step.
//
// The precise, narrower forms remain available: Fork and ForkDaemon as package
// functions, and Await, Join and Interrupt as methods on Fiber.

// Fork starts fx on its own goroutine owned by the current dynamic scope, in
// these channels.
func (Operations[R, E]) Fork[A any](fx Effect[R, E, A]) Effect[R, E, Fiber[E, A]] {
	return WidenError[E](Fork(fx))
}

// ForkDaemon starts fx owned by the Runtime root scope, in these channels.
func (Operations[R, E]) ForkDaemon[A any](fx Effect[R, E, A]) Effect[R, E, Fiber[E, A]] {
	return WidenError[E](ForkDaemon(fx))
}

// ForkIn starts fx owned by scope rather than by the current dynamic scope, in
// these channels.
func (Operations[R, E]) ForkIn[A any](scope Scope, fx Effect[R, E, A]) Effect[R, E, Fiber[E, A]] {
	return WidenError[E](scope.Fork(fx))
}

// Await waits for the fiber and yields its complete outcome, in these channels.
func (Operations[R, E]) Await[A any](fiber Fiber[E, A]) Effect[R, E, Exit[E, A]] {
	return WidenError[E](fiber.Await[R]())
}

// Join waits for the fiber and adopts its outcome, in these channels.
func (Operations[R, E]) Join[A any](fiber Fiber[E, A]) Effect[R, E, A] {
	return fiber.Join[R]()
}

// Interrupt cancels the fiber, waits for its cleanup, and yields its terminal
// outcome, in these channels.
func (Operations[R, E]) Interrupt[A any](fiber Fiber[E, A]) Effect[R, E, Exit[E, A]] {
	return WidenError[E](fiber.Interrupt[R]())
}
