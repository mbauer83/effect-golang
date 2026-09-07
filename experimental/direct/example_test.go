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

// A dependent sequence reads as ordinary Go: each Bind returns a value the next
// line can use.
func ExampleRun() {
	operations := effect.For[effect.Unit, string]()

	program := direct.Run(func(bind *direct.Binder[effect.Unit, string]) string {
		greeting := direct.Bind(bind, operations.Succeed("hello"))
		subject := direct.Bind(bind, operations.Succeed("world"))
		return greeting + ", " + subject
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	value, _ := exit.Value()
	fmt.Println(value)
	// Output: hello, world
}

// A failing Bind abandons the rest of the body, so later steps do not run and
// the failure reaches the effect's error channel unchanged.
func ExampleBind_shortCircuit() {
	operations := effect.For[effect.Unit, string]()

	program := direct.Run(func(bind *direct.Binder[effect.Unit, string]) string {
		first := direct.Bind(bind, operations.Succeed("loaded"))
		direct.Bind(bind, operations.Fail[string]("catalogue unavailable"))
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
func ExampleBind_defect() {
	operations := effect.For[effect.Unit, string]()

	program := direct.Run(func(bind *direct.Binder[effect.Unit, string]) string {
		return direct.Bind(bind, operations.From(
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

// A recover() in the body can swallow the short-circuit. That cannot be
// prevented, but it is detected: reporting a defect is better than returning a
// value the program never computed.
func ExampleRun_swallowedShortCircuit() {
	operations := effect.For[effect.Unit, string]()

	program := direct.Run(func(bind *direct.Binder[effect.Unit, string]) (result string) {
		defer func() {
			if recover() != nil {
				result = "recovered"
			}
		}()
		return direct.Bind(bind, operations.Fail[string]("rejected"))
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	_, succeeded := exit.Value()
	cause, _ := exit.Cause()
	fmt.Println("returned a value:", succeeded)
	fmt.Println("contains a defect:", cause.ContainsDefect())
	// Output:
	// returned a value: false
	// contains a defect: true
}

// A bound effect runs inside the surrounding interpretation, so it sees the
// runtime's capabilities and the enclosing scope. Here the resource is released
// by the scope, not by the body.
func ExampleBind_scoped() {
	operations := effect.For[effect.Unit, string]()

	program := effect.Scoped(func(scope effect.Scope) effect.Effect[effect.Unit, string, string] {
		return direct.Run(func(bind *direct.Binder[effect.Unit, string]) string {
			handle := direct.Bind(bind, scope.AcquireRelease(
				operations.Succeed("connection"),
				func(held string) effect.Effect[effect.Unit, effect.Never, effect.Unit] {
					return effect.Release[effect.Unit](func(context.Context) error {
						fmt.Println("released", held)
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
