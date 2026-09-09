// Package benchmark measures the sequencing styles against each other.
//
// The architecture plan gates direct style on its measured cost rather than on
// taste: it pays for a panic on the expected-failure path, and the question is
// how much. These benchmarks answer that with the same three-step dependent
// workflow written three ways.
package benchmark

import (
	"context"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/experimental/direct"
)

type step = effect.Effect[effect.Unit, string, int]

var operations = effect.For[effect.Unit, int]()

func loadFirst() step          { return effect.Succeed[effect.Unit, string](1) }
func loadSecond(from int) step { return effect.Succeed[effect.Unit, string](from + 1) }
func loadThird(from int) step  { return effect.Succeed[effect.Unit, string](from + 1) }

func rejectAtSecond(int) step { return effect.Fail[effect.Unit, int]("rejected") }

// state is the workflow builder's caller-declared state.
type state struct {
	first  int
	second int
	third  int
}

func flatMapped(second func(int) step) step {
	return loadFirst().FlatMap(func(first int) step {
		return second(first).FlatMap(func(value int) step {
			return loadThird(value)
		})
	})
}

func workflowed(second func(int) step) step {
	return effect.NewWorkflow[effect.Unit, string](func() state { return state{} }).
		Bind(
			func(state) step { return loadFirst() },
			func(current state, value int) state { current.first = value; return current },
		).
		Bind(
			func(current state) step { return second(current.first) },
			func(current state, value int) state { current.second = value; return current },
		).
		Bind(
			func(current state) step { return loadThird(current.second) },
			func(current state, value int) state { current.third = value; return current },
		).
		Yield(func(current state) int { return current.third })
}

func directed(second func(int) step) step {
	return direct.Run(func(bind *direct.Binder[effect.Unit, string]) int {
		first := direct.Bind(bind, loadFirst())
		value := direct.Bind(bind, second(first))
		return direct.Bind(bind, loadThird(value))
	})
}

func measure(b *testing.B, program step) {
	b.Helper()
	b.ReportAllocs()
	runtime, err := effect.NewRuntime()
	if err != nil {
		b.Fatal(err)
	}
	defer runtime.Close(context.Background())

	ctx := context.Background()
	for b.Loop() {
		runtime.Run(ctx, effect.Unit{}, program)
	}
}

func BenchmarkSucceedingFlatMap(b *testing.B)  { measure(b, flatMapped(loadSecond)) }
func BenchmarkSucceedingWorkflow(b *testing.B) { measure(b, workflowed(loadSecond)) }
func BenchmarkSucceedingDirect(b *testing.B)   { measure(b, directed(loadSecond)) }

func BenchmarkFailingFlatMap(b *testing.B)  { measure(b, flatMapped(rejectAtSecond)) }
func BenchmarkFailingWorkflow(b *testing.B) { measure(b, workflowed(rejectAtSecond)) }
func BenchmarkFailingDirect(b *testing.B)   { measure(b, directed(rejectAtSecond)) }
