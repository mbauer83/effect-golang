package cases

// A body whose last step is followed only by statements and a value.

import (
	"fmt"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
)

// After the last step, the statements that follow and the value returned are
// one mapping; a closure made there sees what those statements assigned.
func TestTheStatementsAfterTheLastStepRunAndTheirValueIsReturned(t *testing.T) {
	value, _ := run(t, effect.Gen(func(do *body) string {
		prefix := "a"
		x := do.Await(ops.Succeed(1))
		prefix += fmt.Sprint(x)
		read := func() string { return prefix }
		do.Await(ops.Succeed(2))
		return read() + "!"
	}))
	want(t, value, "a1!")
}

func TestALastStepThatFailsEndsTheBodyWithItsFailure(t *testing.T) {
	reached := false
	_, failure := run(t, effect.Gen(func(do *body) int {
		x := do.Await(ops.Succeed(1))
		y := do.Await(ops.Fail[int]("refused"))
		reached = true
		return x + y
	}))
	want(t, failure, "refused")
	want(t, reached, false)
}

func TestTheMappingRunsOncePerInterpretation(t *testing.T) {
	runs := 0
	program := effect.Gen(func(do *body) int {
		x := do.Await(ops.Succeed(1))
		runs++
		return x + runs
	})
	first, _ := run(t, program)
	second, _ := run(t, program)
	want(t, first+second, 5)
}

// An if whose init awaits and whose branches do not: the init is awaited
// first, and the if is written without it.
func TestAnIfWhoseInitAwaitsDecidesOnWhatItAwaited(t *testing.T) {
	answer := func(held int) effect.Effect[effect.Unit, string, int] {
		return effect.Gen(func(do *body) int {
			if found := do.Await(ops.Succeed(held)); found > 0 {
				return found
			}
			return -1
		})
	}
	first, _ := run(t, answer(3))
	second, _ := run(t, answer(0))
	want(t, first+second, 2)
}

func TestASwitchWhoseInitAwaitsDecidesOnWhatItAwaited(t *testing.T) {
	value, _ := run(t, effect.Gen(func(do *body) string {
		switch held := do.Await(ops.Succeed(2)); held {
		case 2:
			return "two"
		}
		return "other"
	}))
	want(t, value, "two")
}
