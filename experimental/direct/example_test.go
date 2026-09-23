package direct_test

// These are documentation. Go renders an Example only when it sits beside the
// package it documents, so unlike the behaviour tests in test/unit these live
// here.

import (
	"context"
	"fmt"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/experimental/direct"
)

// A dependent sequence reads as ordinary Go: each Await returns a value the next
// line can use.
func ExampleRun() {
	operations := effect.For[effect.Unit, string]()

	program := direct.Run(func(do *direct.Do[effect.Unit, string]) string {
		greeting := do.Await(operations.Succeed("hello"))
		subject := do.Await(operations.Succeed("world"))
		return greeting + ", " + subject
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	value, _ := exit.Value()
	fmt.Println(value)
	// Output: hello, world
}

// A failing Await abandons the rest of the body, so later steps do not run and
// the failure reaches the effect's error channel unchanged.
func ExampleDo_Await_shortCircuit() {
	operations := effect.For[effect.Unit, string]()

	program := direct.Run(func(do *direct.Do[effect.Unit, string]) string {
		first := do.Await(operations.Succeed("loaded"))
		do.Await(operations.Fail[string]("catalogue unavailable"))
		fmt.Println("this line never runs")
		return first
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, _ := exit.Cause()
	failure, _ := cause.Failure()
	fmt.Println("failed with:", failure)
	// Output: failed with: catalogue unavailable
}

// A defect and an interruption abandon the body too, and neither is turned into
// a typed failure on the way out.
func ExampleDo_Await_defect() {
	operations := effect.For[effect.Unit, string]()

	program := direct.Run(func(do *direct.Do[effect.Unit, string]) string {
		return do.Await(operations.From(
			func(context.Context, effect.Unit) effect.Exit[string, string] {
				panic("index out of range")
			},
		))
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, _ := exit.Cause()
	fmt.Println("typed failures:", len(cause.Failures()))
	fmt.Println("contains a defect:", cause.ContainsDefect())
	// Output:
	// typed failures: 0
	// contains a defect: true
}

// A failure runs the body's deferred calls, as a finally block would, and a
// recover() among them finds nothing to recover: the failure is not a panic.
func ExampleDo_Await_deferred() {
	operations := effect.For[effect.Unit, string]()

	program := direct.Run(func(do *direct.Do[effect.Unit, string]) string {
		defer func() {
			fmt.Println("recovered:", recover())
		}()
		return do.Await(operations.Fail[string]("rejected"))
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, _ := exit.Cause()
	failure, _ := cause.Failure()
	fmt.Println("failed with:", failure)
	// Output:
	// recovered: <nil>
	// failed with: rejected
}

// An awaited effect runs inside the surrounding interpretation, so it sees the
// runtime's capabilities and the enclosing scope. Here the resource is released
// by the scope, not by the body.
func ExampleDo_Await_scoped() {
	operations := effect.For[effect.Unit, string]()

	program := effect.Scoped(func(scope effect.Scope) effect.Effect[effect.Unit, string, string] {
		return direct.Run(func(do *direct.Do[effect.Unit, string]) string {
			handle := do.Await(scope.AcquireRelease(
				operations.Succeed("connection"),
				func(resource string) effect.Effect[effect.Unit, effect.Never, effect.Unit] {
					return effect.AddFinalizer[effect.Unit](func(context.Context) error {
						fmt.Println("released", resource)
						return nil
					})
				},
			))
			return "used " + handle
		})
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	value, _ := exit.Value()
	fmt.Println(value)
	// Output:
	// released connection
	// used connection
}
