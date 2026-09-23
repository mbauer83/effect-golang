package benchmark

// What observation costs when nobody is observing.
//
// The claim worth measuring rather than asserting: a program that named no
// observer should pay a nil check per boundary and nothing else -- no event
// built, no attributes cloned, no clock read it would not have read anyway.

import (
	"context"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
)

// spanWorkload is a span-heavy workload, which is where observation costs what it
// costs: the boundaries are the events.
func spanWorkload(depth int) effect.Effect[effect.Unit, effect.Never, int] {
	work := effect.Succeed[effect.Unit, effect.Never](0)
	for at := range depth {
		named := "stage"

		work = work.
			Map(func(total int) int { return total + at }).
			WithSpan(named)
	}
	return work
}

func runSpans(b *testing.B, runtime *effect.Runtime) {
	b.Helper()
	work := spanWorkload(64)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		runtime.Run(context.Background(), effect.Unit{}, work)
	}
}

func BenchmarkSixtyFourSpansUnobserved(b *testing.B) {
	runtime, err := effect.NewRuntime()
	if err != nil {
		b.Fatal(err)
	}
	runSpans(b, runtime)
}

func BenchmarkSixtyFourSpansObserved(b *testing.B) {
	runtime, err := effect.NewRuntime(effect.WithObserver(eventCounter{}))
	if err != nil {
		b.Fatal(err)
	}
	runSpans(b, runtime)
}

// eventCounter is the cheapest possible observer, so what the comparison shows is
// the cost of observing at all rather than the cost of one tool.
type eventCounter struct{}

func (eventCounter) Observe(context.Context, effect.RuntimeEvent) {}
