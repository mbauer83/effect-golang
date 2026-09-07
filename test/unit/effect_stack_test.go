package unit

import (
	"context"
	"strings"
	"testing"

	effect "github.com/mbauer83/effect-golang"
)

// The closure evaluator this runtime replaced overflowed Go's 1 GB goroutine
// stack at one million sequential operations. The instruction interpreter
// records continuations on the heap, so these cases are bounded by memory
// rather than by stack depth.
const (
	shallowDepth = 100_000
	deepDepth    = 1_000_000
)

// The retry loop's own stack behaviour is covered alongside the rest of retry,
// in retry_boundaries_test.go.

func TestDeepSequentialComposition(t *testing.T) {
	for _, depth := range []int{shallowDepth, deepDepth} {
		operations := effect.For[effect.Unit, string]()
		program := operations.Succeed(0)
		for range depth {
			program = program.Map(func(value int) int {
				return value + 1
			})
		}

		exit := effect.Run(context.Background(), effect.Unit{}, program)
		value, ok := exit.Value()
		if !ok || value != depth {
			t.Fatalf("unexpected result for %d maps: %v", depth, exit)
		}
	}
}

func TestDeepFlatMapComposition(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	program := operations.Succeed(0)
	for range deepDepth {
		program = program.FlatMap(func(value int) effect.Effect[effect.Unit, string, int] {
			return operations.Succeed(value + 1)
		})
	}

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	value, ok := exit.Value()
	if !ok || value != deepDepth {
		t.Fatalf("unexpected result for %d binds: %v", deepDepth, exit)
	}
}

func TestDeepCauseTraversalRemainsTotal(t *testing.T) {
	cause := effect.FailCause("root")
	for range shallowDepth {
		cause = cause.Then(effect.FailCause("cleanup"))
	}

	if got := len(cause.Failures()); got != shallowDepth+1 {
		t.Fatalf("expected %d failures, got %d", shallowDepth+1, got)
	}
	folded := cause.Fold(effect.CauseFolder[string, int]{
		Empty:        func() int { return 0 },
		Failure:      func(string) int { return 1 },
		Defect:       func(effect.Defect) int { return 0 },
		Interruption: func(effect.Interruption) int { return 0 },
		Then:         func(left, right int) int { return left + right },
		Both:         func(left, right int) int { return left + right },
	})
	if folded != shallowDepth+1 {
		t.Fatalf("expected %d folded leaves, got %d", shallowDepth+1, folded)
	}
}

// Rendering indents by depth, so its output is inherently quadratic in the
// depth of the tree. The renderer is iterative, which is what keeps a deep
// cause renderable at all; this bounds the case to a realistic finalizer chain.
func TestDeepCauseRenderingRemainsTotal(t *testing.T) {
	const depth = 1_000
	cause := effect.FailCause("root")
	for range depth {
		cause = cause.Then(effect.DieCause[string](effect.Defect{Value: "cleanup"}))
	}

	rendered := cause.String()
	if got := strings.Count(rendered, "Die(cleanup)"); got != depth {
		t.Fatalf("expected %d rendered defects, got %d", depth, got)
	}
}
