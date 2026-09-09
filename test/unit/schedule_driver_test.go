package unit

// Schedule driver laws are stated over a driver directly rather than through
// Retry, because asserting them through Retry would test the retry loop as much
// as the policy. Starting a driver is part of the public surface for exactly
// this reason: a caller composing policies needs to check what they decided
// without running any effects.

import (
	"testing"
	"time"

	"github.com/mbauer83/effect-golang/effect"
)

// noMoment is the clock reading for a policy whose decisions do not depend on
// elapsed time.
var noMoment = time.Time{}

func TestFibonacciBackoffIsCappedWithoutOverflow(t *testing.T) {
	driver := effect.Fibonacci[string](2*time.Second, 5*time.Second).Start()
	want := []time.Duration{2 * time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second}
	for index, expected := range want {
		decision := driver.Next(noMoment, "failure")
		if !decision.Continues() || decision.Delay() != expected || decision.Output() != expected {
			t.Fatalf("decision %d: got %+v, want delay %v", index, decision, expected)
		}
	}

	// A first delay near the maximum representable duration must not wrap.
	large := time.Duration(1 << 62)
	decision := effect.Fibonacci[string](large, time.Duration(1<<63-1)).Start().Next(noMoment, "failure")
	if decision.Delay() != large {
		t.Fatalf("large initial delay changed: %v", decision.Delay())
	}
}

func TestUpToUsesDriverStartAndElapsedBoundary(t *testing.T) {
	started := time.Unix(10, 0)
	driver := effect.UpTo[string](5 * time.Second).Start()
	first := driver.Next(started, "first")
	inside := driver.Next(started.Add(4*time.Second), "second")
	boundary := driver.Next(started.Add(5*time.Second), "third")

	if !first.Continues() || first.Output() != 0 {
		t.Fatalf("unexpected initial elapsed decision: %+v", first)
	}
	if !inside.Continues() || inside.Output() != 4*time.Second {
		t.Fatalf("unexpected inside decision: %+v", inside)
	}
	if boundary.Continues() || boundary.Output() != 5*time.Second {
		t.Fatalf("unexpected boundary decision: %+v", boundary)
	}
}

func TestSchedulePredicatesStopAtRejectedInputOrOutput(t *testing.T) {
	byInput := effect.Forever[int]().WhileInput(func(input int) bool { return input < 2 }).Start()
	accepted := byInput.Next(noMoment, 1)
	rejected := byInput.Next(noMoment, 2)
	if !accepted.Continues() || rejected.Continues() {
		t.Fatalf("unexpected input predicate decisions: accepted=%+v rejected=%+v", accepted, rejected)
	}

	byOutput := effect.Forever[int]().WhileOutput(func(output uint64) bool { return output < 2 }).Start()
	first := byOutput.Next(noMoment, 0)
	second := byOutput.Next(noMoment, 0)
	third := byOutput.Next(noMoment, 0)
	if !first.Continues() || !second.Continues() || third.Continues() {
		t.Fatalf("unexpected output predicate decisions: %+v %+v %+v", first, second, third)
	}
}

func TestAndSchedulesUsesIntersectionLaws(t *testing.T) {
	driver := effect.IntersectSchedules(
		effect.Recurs[string](1),
		effect.Spaced[string](3*time.Second),
	).Start()
	first := driver.Next(noMoment, "failure")
	second := driver.Next(noMoment, "failure")

	if !first.Continues() || first.Delay() != 3*time.Second {
		t.Fatalf("intersection did not select the longest delay: %+v", first)
	}
	if second.Continues() {
		t.Fatalf("intersection continued after one side stopped: %+v", second)
	}
}

func TestOrSchedulesUsesUnionLaws(t *testing.T) {
	driver := effect.UnionSchedules(
		effect.Recurs[string](1),
		effect.Spaced[string](3*time.Second).WhileOutput(func(output uint64) bool { return output < 2 }),
	).Start()
	first := driver.Next(noMoment, "failure")
	second := driver.Next(noMoment, "failure")
	third := driver.Next(noMoment, "failure")

	if !first.Continues() || first.Delay() != 0 || !first.Output().LeftContinues || !first.Output().RightContinues {
		t.Fatalf("unexpected first union decision: %+v", first)
	}
	if !second.Continues() || second.Delay() != 3*time.Second ||
		second.Output().LeftContinues || !second.Output().RightContinues {
		t.Fatalf("unexpected second union decision: %+v", second)
	}
	if third.Continues() || third.Output().LeftContinues || third.Output().RightContinues {
		t.Fatalf("unexpected terminal union decision: %+v", third)
	}
}

func TestJitteredUsesInjectedBoundedRandomness(t *testing.T) {
	checks := []struct {
		name     string
		fraction float64
		want     time.Duration
	}{
		{name: "lower clamp", fraction: -1, want: 5 * time.Second},
		{name: "middle", fraction: 0.5, want: 10 * time.Second},
		{name: "upper clamp", fraction: 2, want: 15 * time.Second},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			policy := effect.Spaced[string](10*time.Second).Jittered(
				func() float64 { return check.fraction },
				0.5,
				1.5,
			)
			decision := policy.Start().Next(noMoment, "failure")
			if decision.Delay() != check.want {
				t.Fatalf("got jittered delay %v, want %v", decision.Delay(), check.want)
			}
		})
	}
}

func TestOneScheduleValueStartsIndependentDrivers(t *testing.T) {
	// This is the invariant that makes a Schedule safe to share: the value holds
	// no run state, so two drivers started from it cannot influence each other.
	policy := effect.Recurs[string](2)
	first := policy.Start()
	second := policy.Start()

	first.Next(noMoment, "failure")
	first.Next(noMoment, "failure")
	if exhausted := first.Next(noMoment, "failure"); exhausted.Continues() {
		t.Fatalf("expected the first driver to be exhausted: %+v", exhausted)
	}
	if fresh := second.Next(noMoment, "failure"); !fresh.Continues() {
		t.Fatalf("a second driver inherited the first's progress: %+v", fresh)
	}
}
