package effect

// This is the module's only test inside a production package.
//
// Schedule driver laws are stated over the private driver directly, because
// asserting them through Retry would test the retry loop as much as the policy.
// Everything that can be stated over the public API lives under test/ instead.

import (
	"testing"
	"time"
)

func TestFibonacciBackoffIsCappedWithoutOverflow(t *testing.T) {
	step := Fibonacci[string](2*time.Second, 5*time.Second).driver()
	want := []time.Duration{2 * time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second}
	for index, expected := range want {
		decision, next := step(time.Time{}, "failure")
		if !decision.Continues() || decision.Delay() != expected || decision.Output() != expected {
			t.Fatalf("decision %d: got %+v, want delay %v", index, decision, expected)
		}
		step = next
	}

	large := time.Duration(1 << 62)
	decision, _ := Fibonacci[string](large, time.Duration(1<<63-1)).driver()(time.Time{}, "failure")
	if decision.Delay() != large {
		t.Fatalf("large initial delay changed: %v", decision.Delay())
	}
}

func TestUpToUsesDriverStartAndElapsedBoundary(t *testing.T) {
	started := time.Unix(10, 0)
	step := UpTo[string](5 * time.Second).driver()
	first, step := step(started, "first")
	inside, step := step(started.Add(4*time.Second), "second")
	boundary, _ := step(started.Add(5*time.Second), "third")

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
	inputStep := Forever[int]().WhileInput(func(input int) bool { return input < 2 }).driver()
	accepted, inputStep := inputStep(time.Time{}, 1)
	rejected, _ := inputStep(time.Time{}, 2)
	if !accepted.Continues() || rejected.Continues() {
		t.Fatalf("unexpected input predicate decisions: accepted=%+v rejected=%+v", accepted, rejected)
	}

	outputStep := Forever[int]().WhileOutput(func(output uint64) bool { return output < 2 }).driver()
	first, outputStep := outputStep(time.Time{}, 0)
	second, outputStep := outputStep(time.Time{}, 0)
	third, _ := outputStep(time.Time{}, 0)
	if !first.Continues() || !second.Continues() || third.Continues() {
		t.Fatalf("unexpected output predicate decisions: %+v %+v %+v", first, second, third)
	}
}

func TestAndSchedulesUsesIntersectionLaws(t *testing.T) {
	step := AndSchedules(Recurs[string](1), Spaced[string](3*time.Second)).driver()
	first, step := step(time.Time{}, "failure")
	second, _ := step(time.Time{}, "failure")
	if !first.Continues() || first.Delay() != 3*time.Second {
		t.Fatalf("intersection did not select longest delay: %+v", first)
	}
	if second.Continues() {
		t.Fatalf("intersection continued after one side stopped: %+v", second)
	}
}

func TestOrSchedulesUsesUnionLaws(t *testing.T) {
	step := OrSchedules(
		Recurs[string](1),
		Spaced[string](3*time.Second).WhileOutput(func(output uint64) bool { return output < 2 }),
	).driver()
	first, step := step(time.Time{}, "failure")
	second, step := step(time.Time{}, "failure")
	third, _ := step(time.Time{}, "failure")

	if !first.Continues() || first.Delay() != 0 || !first.Output().LeftContinues || !first.Output().RightContinues {
		t.Fatalf("unexpected first union decision: %+v", first)
	}
	if !second.Continues() || second.Delay() != 3*time.Second || second.Output().LeftContinues || !second.Output().RightContinues {
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
			policy := Spaced[string](10*time.Second).Jittered(
				func() float64 { return check.fraction },
				0.5,
				1.5,
			)
			decision, _ := policy.driver()(time.Time{}, "failure")
			if decision.Delay() != check.want {
				t.Fatalf("got jittered delay %v, want %v", decision.Delay(), check.want)
			}
		})
	}
}
