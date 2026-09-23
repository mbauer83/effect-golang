package unit

// Failing out of a direct-style body.

import (
	"context"
	"fmt"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
)

// A guard clause is how every judgement in a step is written: this cannot
// proceed, so say why and stop. Binding a failure works and names a success
// type the failure does not have, so Fail is the same short-circuit with the
// type gone.
func TestAGuardClauseAbandonsTheBodyWithItsFailure(t *testing.T) {
	refused := effect.Gen(func(do *effect.Do[effect.Unit, string]) int {
		if true {
			do.Fail("the aggregate refused")
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
	guarded := effect.Gen(func(do *effect.Do[effect.Unit, string]) int {
		do.Fail("stop here")
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

func TestAGuardClauseRecordsTheLineThatWroteIt(t *testing.T) {
	var line int
	program := effect.Gen(func(do *effect.Do[effect.Unit, string]) int {
		_, _, line, _ = goruntime.Caller(0)
		do.Fail("refused") // the line after the one Caller reported
		return 0
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, _ := exit.Cause()
	want := fmt.Sprintf("gen_guard_test.go:%d", line+1)
	if !strings.HasSuffix(cause.Origin().Source, want) {
		t.Fatalf("expected the origin to be %s, got %q", want, cause.Origin().Source)
	}
}
