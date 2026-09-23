// Package benchmark measures the sequencing styles against each other.
//
// Direct style costs a goroutine hand-off per run, and effectgo's rewrite is
// meant to remove it, so both claims are measured rather than assumed. These
// benchmarks run the same three-step dependent workflow three ways: as a
// FlatMap chain built once, as one built per run, and in direct style -- which
// is its rewritten form when run under effectgo's overlay.
package benchmark

import (
	"context"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
)

type step = effect.Effect[effect.Unit, string, int]

var operations = effect.For[effect.Unit, int]()

func loadFirst() step          { return effect.Succeed[effect.Unit, string](1) }
func loadSecond(from int) step { return effect.Succeed[effect.Unit, string](from + 1) }
func loadThird(from int) step  { return effect.Succeed[effect.Unit, string](from + 1) }

func rejectAtSecond(int) step { return effect.Fail[effect.Unit, int]("rejected") }

func sequenceFlatMap(second func(int) step) step {
	return loadFirst().FlatMap(func(first int) step {
		return second(first).FlatMap(func(value int) step {
			return loadThird(value)
		})
	})
}

// sequenceFlatMapPerRun is the same chain built once per interpretation, which
// is what a direct-style body is: its locals are fresh for every run, so a
// retry or a concurrent run shares nothing. It is the fair baseline for direct
// style rewritten by effectgo, where sequenceFlatMap reuses a chain built once.
func sequenceFlatMapPerRun(second func(int) step) step {
	return effect.Suspend(func() step { return sequenceFlatMap(second) })
}

func sequenceDirect(second func(int) step) step {
	return effect.Gen(func(do *effect.Do[effect.Unit, string]) int {
		first := do.Await(loadFirst())
		value := do.Await(second(first))
		return do.Await(loadThird(value))
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

func BenchmarkSucceedingFlatMap(b *testing.B) { measure(b, sequenceFlatMap(loadSecond)) }
func BenchmarkSucceedingFlatMapPerRun(b *testing.B) {
	measure(b, sequenceFlatMapPerRun(loadSecond))
}
func BenchmarkSucceedingDirect(b *testing.B) { measure(b, sequenceDirect(loadSecond)) }

func BenchmarkFailingFlatMap(b *testing.B) { measure(b, sequenceFlatMap(rejectAtSecond)) }
func BenchmarkFailingFlatMapPerRun(b *testing.B) {
	measure(b, sequenceFlatMapPerRun(rejectAtSecond))
}
func BenchmarkFailingDirect(b *testing.B) { measure(b, sequenceDirect(rejectAtSecond)) }
