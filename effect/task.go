package effect

import (
	"github.com/mbauer83/effect-golang/effect/fault"
	"github.com/mbauer83/effect-golang/effect/internal/outcome"
)

// Task is the effect most of an application is written in: it needs no
// environment, because an adapter closes over the client it was built with,
// and it fails with a fault.Fault, whose kind is what a caller decides on.
// ZIO calls the same shape IO; Task here, because effect.IO names the
// filesystem operations.
type Task[A any] = Effect[Unit, fault.Fault, A]

// TaskDo is the receiver of a task's steps in direct style:
//
//	effect.Gen(func(do *effect.TaskDo) Session {
//		id := do.Await(codes.Redeem(code))
//		return do.Await(sessions.Find(id))
//	})
type TaskDo = Do[Unit, fault.Fault]

// FromValue is a task whose result is already known.
func FromValue[A any](value A) Task[A] {
	return Succeed[Unit, fault.Fault](value)
}

// FromFault is a task that fails without doing anything. The failure records
// its caller's line, as Fail does.
func FromFault[A any](why fault.Fault) Task[A] {
	return FailWithCause[Unit, A](Cause[fault.Fault]{
		node: outcome.FailCause(why).WithOrigin(outcome.Origin{Source: callSite(2)}),
	})
}

// Done is the task of a step with nothing to do.
func Done() Task[Unit] { return FromValue(Unit{}) }

// TaskOperations are the standard operations -- the clock, logging, forking --
// in a task's channels.
func TaskOperations() Operations[Unit, fault.Fault] { return For[Unit, fault.Fault]() }
