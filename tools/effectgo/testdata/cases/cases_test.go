// Package cases holds direct-style bodies chosen for what a rewrite into
// FlatMap chains could get wrong. The same tests run on the source as written
// and on the rewritten source, and must pass both ways.
package cases

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
)

type body = effect.Do[effect.Unit, string]

var ops = effect.For[effect.Unit, string]()

func run[A any](t *testing.T, fx effect.Effect[effect.Unit, string, A]) (A, string) {
	t.Helper()
	exit := effect.Run(context.Background(), effect.Unit{}, fx)
	if value, ok := exit.Value(); ok {
		return value, ""
	}
	cause, _ := exit.Cause()
	if failure, ok := cause.Failure(); ok {
		var zero A
		return zero, failure
	}
	t.Fatalf("neither a value nor a typed failure: %v", exit)
	panic("unreachable")
}

func want[A comparable](t *testing.T, got, expected A) {
	t.Helper()
	if got != expected {
		t.Fatalf("got %v, want %v", got, expected)
	}
}

// A := that reuses a name assigns it, and a closure made before sees that.
func TestRedeclarationAssignsTheEarlierVariable(t *testing.T) {
	value, _ := run(t, effect.Gen(func(do *body) int {
		a := 1
		read := func() int { return a }
		x := do.Await(ops.Succeed(1))
		a, b := x+1, 2
		return read() + b
	}))
	want(t, value, 4)
}

func TestAnInnerScopeShadowsWithoutLeaking(t *testing.T) {
	value, _ := run(t, effect.Gen(func(do *body) int {
		x := do.Await(ops.Succeed(1))
		{
			x := do.Await(ops.Succeed(20))
			_ = x
		}
		return x
	}))
	want(t, value, 1)
}

func TestBranchesRejoinWhatFollows(t *testing.T) {
	for input, expected := range map[int]string{0: "zero!", 1: "one!", 5: "many!"} {
		value, _ := run(t, effect.Gen(func(do *body) string {
			n := do.Await(ops.Succeed(input))
			label := "many"
			if n == 0 {
				label = do.Await(ops.Succeed("zero"))
			} else if n == 1 {
				label = do.Await(ops.Succeed("one"))
			}
			return label + do.Await(ops.Succeed("!"))
		}))
		want(t, value, expected)
	}
}

func TestAnEarlyReturnAfterAStep(t *testing.T) {
	value, _ := run(t, effect.Gen(func(do *body) string {
		n := do.Await(ops.Succeed(3))
		if n > 2 {
			return "early"
		}
		return do.Await(ops.Succeed("late"))
	}))
	want(t, value, "early")
}

// A break inside a translated switch leaves the switch, even from inside an if
// that is itself inside a continuation.
func TestABreakLeavesTheSwitch(t *testing.T) {
	value, _ := run(t, effect.Gen(func(do *body) string {
		trail := ""
		switch do.Await(ops.Succeed(2)) {
		case 1:
			trail += "one"
		case 2:
			n := do.Await(ops.Succeed(7))
			if n > 5 {
				trail += "big"
				break
			}
			trail += "small"
		default:
			trail += "other"
		}
		return trail + "."
	}))
	want(t, value, "big.")
}

func TestATypeSwitchWithAStepInAClause(t *testing.T) {
	value, _ := run(t, effect.Gen(func(do *body) string {
		var thing any = do.Await(ops.Succeed(42))
		switch v := thing.(type) {
		case string:
			return v
		case int:
			return fmt.Sprint(v + do.Await(ops.Succeed(1)))
		}
		return "none"
	}))
	want(t, value, "43")
}

// Steps in one expression are awaited left to right, arguments first.
func TestStepsInOneExpressionKeepTheirOrder(t *testing.T) {
	var order []string
	step := func(name string, value int) effect.Effect[effect.Unit, string, int] {
		return ops.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
			order = append(order, name)
			return effect.ExitSuccess[string](value)
		})
	}
	value, _ := run(t, effect.Gen(func(do *body) int {
		return do.Await(step("a", 1)) + do.Await(step("b", do.Await(step("c", 2))))*10
	}))
	want(t, value, 21)
	want(t, strings.Join(order, ""), "acb")
}

func TestAStepInAnIfInitKeepsItsScope(t *testing.T) {
	value, _ := run(t, effect.Gen(func(do *body) int {
		v := 100
		if v := do.Await(ops.Succeed(1)); v > 0 {
			v += do.Await(ops.Succeed(1))
			_ = v
		}
		return v
	}))
	want(t, value, 100)
}

func TestANestedBodyIsRewrittenToo(t *testing.T) {
	value, _ := run(t, effect.Gen(func(do *body) int {
		inner := effect.Gen(func(do *body) int {
			return do.Await(ops.Succeed(2)) * 3
		})
		return do.Await(inner) + 1
	}))
	want(t, value, 7)
}

// A body that declares the name the generated code would use for the effect
// package must not have it shadowed.
func TestABodyThatShadowsThePackageName(t *testing.T) {
	value, _ := run(t, effect.Gen(func(do *body) string {
		effect := do.Await(ops.Succeed("shadow"))
		return effect + do.Await(ops.Succeed("ed"))
	}))
	want(t, value, "shadowed")
}

func TestAGuardClause(t *testing.T) {
	_, failure := run(t, effect.Gen(func(do *body) int {
		if do.Await(ops.Succeed(0)) == 0 {
			do.Fail("empty")
		}
		return 1
	}))
	want(t, failure, "empty")
}

func TestDeclarationsAndAssignments(t *testing.T) {
	value, _ := run(t, effect.Gen(func(do *body) int {
		var x, y = do.Await(ops.Succeed(1)), 5
		x = do.Await(ops.Succeed(10))
		x += do.Await(ops.Succeed(100))
		return x + y
	}))
	want(t, value, 115)
}

func generic[R any](value R) effect.Effect[effect.Unit, string, []R] {
	return effect.Gen(func(do *effect.Do[effect.Unit, string]) []R {
		first := do.Await(effect.Succeed[effect.Unit, string](value))
		return []R{first, do.Await(effect.Succeed[effect.Unit, string](value))}
	})
}

func TestAGenericBody(t *testing.T) {
	value, _ := run(t, generic("x"))
	want(t, strings.Join(value, ""), "xx")
}

func TestAFailureInABranchSkipsTheContinuation(t *testing.T) {
	ran := false
	_, failure := run(t, effect.Gen(func(do *body) int {
		if do.Await(ops.Succeed(true)) {
			do.Await(ops.Fail[int]("stopped"))
		}
		ran = true
		return 0
	}))
	want(t, failure, "stopped")
	want(t, ran, false)
}
