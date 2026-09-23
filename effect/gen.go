package effect

// Direct style: a dependent sequence of effects written as ordinary Go.
//
//	program := effect.Gen(func(do *effect.Do[Env, AppError]) Quote {
//	    customer := do.Await(loadCustomer(id))
//	    basket := do.Await(loadBasket(customer))
//	    if basket.IsEmpty() {
//	        do.Fail(ErrEmptyBasket)
//	    }
//	    return price(customer, basket)
//	})
//
// # How it ends a body early
//
// Await must return an A and must also abandon the body when the effect did
// not succeed, and Go has no resumable suspension. So the body runs on a
// goroutine of its own, in lock step with the interpretation: the
// interpretation waits while the body runs, and the body evaluates every
// effect it awaits with the interpretation's own context, scope and
// capabilities. A failed Await ends the body with runtime.Goexit.
//
// That is what makes it sound where a panic was not. No recover() can catch a
// Goexit, so a body cannot swallow its own failure; and a Goexit runs the
// body's deferred calls, so a defer is a finalizer that runs on failure as on
// success -- what a finally block is in Effect.gen and ensuring is in ZIO.
//
// # What it costs, and where it stops
//
// A run costs a goroutine hand-off on top of what FlatMap costs, about a
// microsecond, and a failed run a fresh goroutine, a few more. Against any real
// work that is noise; for an effect run per element of a hot stream, write
// FlatMap.
//
// Every body holds a goroutine while it runs, and a body that awaits another
// body holds one for each. That is nothing for a handler awaiting a service
// awaiting a repository, and it is the wrong tool for recursion: a program
// that recurses through Gen a million deep holds a million goroutines, where
// the same recursion through FlatMap is stack-safe. Loop with for inside one
// body instead.
//
// The body is not the goroutine that called Gen, so what belongs to a
// goroutine does not carry over: runtime.LockOSThread, profiler labels, and a
// testing.T's FailNow, which ends the body and is reported as a defect.

import (
	"fmt"
	"runtime"
	"strconv"
	"sync/atomic"
)

// Do is a running body's access to the interpretation around it.
//
// It is valid only while that body is running, and only on the body's own
// goroutine. Using it anywhere else reports a defect rather than evaluating an
// effect against an interpretation that has ended or is busy.
type Do[R, E any] struct {
	interpreter Interpreter[R, E]
	cause       Cause[E]
	failed      bool
	live        atomic.Bool
	busy        atomic.Bool
}

// Gen interprets body in direct style. The name is Effect's, whose Effect.gen
// is the same idea written with a generator.
//
// The body runs once per interpretation, so one Gen value is reusable: a retry
// runs the body again, and concurrent runs each have their own.
func Gen[R, E, A any](body func(*Do[R, E]) A) Effect[R, E, A] {
	return WithInterpreter(func(interpreter Interpreter[R, E]) Exit[E, A] {
		do := &Do[R, E]{interpreter: interpreter}
		do.live.Store(true)
		ended := make(chan Exit[E, A], 1)
		dispatchBody(func() { runGenBody(do, body, ended) })
		return <-ended
	})
}

// Await evaluates fx and returns its value.
//
// When fx does not succeed the body ends here: its deferred calls run, and the
// typed failure, defect or interruption reaches the resulting effect
// unchanged.
func (do *Do[R, E]) Await[A any](fx Effect[R, E, A]) A {
	if !do.live.Load() {
		panic(fmt.Errorf("effect: Do used outside the Gen body that created it"))
	}
	if !do.busy.CompareAndSwap(false, true) {
		panic(fmt.Errorf("effect: Do used from two goroutines at once"))
	}
	exit := Interpret(do.interpreter, fx)
	do.busy.Store(false)
	if value, ok := exit.Value(); ok {
		return value
	}
	do.cause, _ = exit.Cause()
	do.failed = true
	runtime.Goexit()
	panic("unreachable: runtime.Goexit returned")
}

// Fail ends the body with this failure.
//
// A guard clause. Every judgement a step makes has this shape -- the aggregate
// refused, so there is nothing to write and nothing to answer with -- and it
// belongs where the judgement is made, not where the body returns.
//
// The failure's origin is the line that called Fail, as it would be for an
// Fail written there.
func (do *Do[R, E]) Fail(failure E) {
	cause := FailCause(failure).WithOrigin(Origin{Source: callerLine()})
	do.Await(FailWithCause[R, Never](cause))
}

// callerLine is the file and line that called the function calling it.
func callerLine() string {
	_, file, line, ok := runtime.Caller(2)
	if !ok {
		return ""
	}
	return file + ":" + strconv.Itoa(line)
}
