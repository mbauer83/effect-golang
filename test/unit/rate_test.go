package unit

// Permission to proceed no faster than agreed.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang/effect/rate"
)

// thrice is an allowance of three every three seconds: a spacing of one
// second, and a burst of three.
func thrice() rate.Allowance {
	return rate.Allowance{Name: "a service", Most: 3, Every: 3 * time.Second}
}

func turned(t *testing.T, limiter rate.Limiter, allowance rate.Allowance) time.Duration {
	t.Helper()
	wait, err := limiter.Turn(context.Background(), allowance)
	if err != nil {
		t.Fatal(err)
	}
	return wait
}

func TestTheBurstAnAllowanceToleratesGoesAtOnce(t *testing.T) {
	limiter := rate.NewHeld(ticking().now)

	for turn := range 3 {
		if wait := turned(t, limiter, thrice()); wait != 0 {
			t.Fatalf("expected turn %d of the burst to go at once, waits %v", turn+1, wait)
		}
	}
}

func TestPastTheBurstEveryTurnIsSpaced(t *testing.T) {
	// What "three every three seconds" means to whoever is being asked: the
	// fourth waits a spacing and the fifth two, and nothing is exceeded and
	// then apologised for.
	limiter := rate.NewHeld(ticking().now)
	for range 3 {
		_ = turned(t, limiter, thrice())
	}

	for turn, expected := range []time.Duration{time.Second, 2 * time.Second} {
		if wait := turned(t, limiter, thrice()); wait != expected {
			t.Fatalf("expected turn %d past the burst to wait %v, waits %v",
				turn+4, expected, wait)
		}
	}
}

func TestASpentAllowanceComesBackOneTurnAtATime(t *testing.T) {
	// Not a window that empties all at once, which is what keeps a burst from
	// arriving on every boundary.
	clock := ticking()
	limiter := rate.NewHeld(clock.now)
	for range 4 {
		_ = turned(t, limiter, thrice())
	}

	clock.past(2 * time.Second)

	if wait := turned(t, limiter, thrice()); wait != 0 {
		t.Fatalf("expected the turn to have come round, waits %v", wait)
	}
}

func TestTwoAllowancesAreCountedApart(t *testing.T) {
	// Two services counted by one limiter must not be confused for one, which
	// is why the allowance carries its own name.
	limiter := rate.NewHeld(ticking().now)
	other := rate.Allowance{Name: "another service", Most: 3, Every: 3 * time.Second}
	for range 4 {
		_ = turned(t, limiter, thrice())
	}

	if wait := turned(t, limiter, other); wait != 0 {
		t.Fatalf("expected the other service's own allowance, waits %v", wait)
	}
}

func TestAnUnstatedAllowanceIsRefusedRatherThanTreatedAsUnlimited(t *testing.T) {
	limiter := rate.NewHeld(ticking().now)

	_, err := limiter.Turn(context.Background(), rate.Allowance{Name: "a service"})

	if !errors.Is(err, rate.ErrUnstated) {
		t.Fatalf("expected an unstated allowance to be refused, got %v", err)
	}
}
