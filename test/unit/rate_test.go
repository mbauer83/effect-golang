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

// turned is a turn taken with no ceiling, which is what every test that is
// not about the ceiling wants.
func turned(t *testing.T, limiter rate.Limiter, allowance rate.Allowance) time.Duration {
	t.Helper()
	wait, err := limiter.Turn(context.Background(), allowance, 0)
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

	_, err := limiter.Turn(context.Background(), rate.Allowance{Name: "a service"}, 0)

	if !errors.Is(err, rate.ErrUnstated) {
		t.Fatalf("expected an unstated allowance to be refused, got %v", err)
	}
}

func TestATurnRefusedForBeingTooFarOffLeavesTheAllowanceAlone(t *testing.T) {
	// The reason the ceiling is the limiter's decision and not its caller's.
	// A caller that asked for a turn and then declined to wait for it would
	// have spent an allowance on a request it never made -- and, worse, moved
	// every caller behind it one spacing further back. Somebody else's
	// allowance is not this program's to burn on requests it abandons.
	limiter := rate.NewHeld(ticking().now)
	allowance := thrice()
	for range 3 {
		_ = turned(t, limiter, allowance)
	}

	// The next turn is one spacing off, and this caller will not wait at all.
	wait, err := limiter.Turn(context.Background(), allowance, time.Millisecond)
	if !errors.Is(err, rate.ErrQueued) {
		t.Fatalf("expected the turn refused as queued, got %v", err)
	}
	if wait != time.Second {
		t.Fatalf("expected the queue reported as one spacing, got %v", wait)
	}

	// So a caller that will wait is still only one spacing off, not two.
	if waited := turned(t, limiter, allowance); waited != time.Second {
		t.Fatalf("the refused turn spent an allowance: a patient caller now waits %v "+
			"rather than the one spacing it should", waited)
	}
}

func TestSpeculativeReadingTakesOnlyTheRoomThatIsFree(t *testing.T) {
	// What keeps a search's backfill from starving the page somebody is
	// looking at. Both state the same allowance; they differ only in how long
	// each will queue for it, and that is enough: the speculative one takes
	// the burst while it is free and is refused the moment there is a queue,
	// leaving every spaced turn for whoever said they would wait.
	limiter := rate.NewHeld(ticking().now)
	allowance := thrice()
	const speculative = 10 * time.Millisecond

	// The burst is free, so speculative work is served from it.
	for range 3 {
		if _, err := limiter.Turn(context.Background(), allowance, speculative); err != nil {
			t.Fatalf("expected the free burst served, got %v", err)
		}
	}
	// Past the burst it yields rather than queueing.
	if _, err := limiter.Turn(context.Background(), allowance, speculative); !errors.Is(err, rate.ErrQueued) {
		t.Fatalf("expected speculative work to yield past the burst, got %v", err)
	}
	// And the work somebody is waiting on is exactly one spacing off, which is
	// where it would have been had the speculative caller never asked.
	if waited := turned(t, limiter, allowance); waited != time.Second {
		t.Fatalf("expected the interactive turn undelayed at one spacing, got %v", waited)
	}
}
