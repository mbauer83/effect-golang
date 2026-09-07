package effect_test

import (
	"context"
	"testing"

	effect "github.com/mbauer83/effect-golang"
)

// Policy behaviour observed through the public retry surface, as an application
// sees it. The driver laws themselves are tested inside the package.

func TestStopNeverRecursAndForeverAlwaysDoes(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	attempts := 0
	flaky := operations.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		attempts++
		return effect.ExitFailure[string, int]("transient")
	})

	exit := effect.Run(context.Background(), effect.Unit{}, flaky.Retry(effect.Stop[string]()))
	if attempts != 1 {
		t.Fatalf("expected Stop to allow only the initial attempt, got %d", attempts)
	}
	if cause, failed := exit.Cause(); !failed || cause.Kind() != effect.CauseFailure {
		t.Fatalf("expected the original failure preserved, got %v", exit)
	}

	// Forever recurs, so it is only safe here bounded by another policy.
	attempts = 0
	bounded := effect.AndSchedules(effect.Forever[string](), effect.Recurs[string](2))
	effect.Run(context.Background(), effect.Unit{}, flaky.Retry(bounded))
	if attempts != 3 {
		t.Fatalf("expected Forever bounded to three attempts, got %d", attempts)
	}
}
