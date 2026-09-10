package unit

// Failing out of a direct-style body.

import (
	"testing"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/experimental/direct"
)

// A guard clause is how every judgement in a step is written: this cannot
// proceed, so say why and stop. Binding a failure works and names a success
// type the failure does not have, so Fail is the same short-circuit with the
// type gone.
func TestAGuardClauseAbandonsTheBodyWithItsFailure(t *testing.T) {
	refused := direct.Run(func(bind *direct.Binder[effect.Unit, string]) int {
		if true {
			direct.Fail(bind, "the aggregate refused")
		}
		return 7
	})

	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	exit := runtime.Run(t.Context(), effect.Unit{}, refused)
	if value, succeeded := exit.Value(); succeeded {
		t.Fatalf("a guarded body answered %v", value)
	}
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("no cause on a failed exit: %+v", exit)
	}
	failures := cause.Failures()
	if len(failures) != 1 || failures[0] != "the aggregate refused" {
		t.Fatalf("the failure did not reach the caller: %+v", exit)
	}
}

// What comes after a guard clause does not run, which is the only reason a
// guard clause is worth having.
func TestNothingAfterAGuardClauseRuns(t *testing.T) {
	reached := false
	guarded := direct.Run(func(bind *direct.Binder[effect.Unit, string]) int {
		direct.Fail(bind, "stop here")
		reached = true
		return 7
	})

	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	runtime.Run(t.Context(), effect.Unit{}, guarded)
	if reached {
		t.Fatal("the body carried on after failing")
	}
}
