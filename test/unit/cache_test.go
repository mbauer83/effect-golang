package unit

// What one process keeps to hand.

import (
	"context"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang/effect/cache"
)

// moving is a clock a test holds.
type moving struct {
	at time.Time
}

func (clock *moving) now() time.Time { return clock.at }

func (clock *moving) past(by time.Duration) { clock.at = clock.at.Add(by) }

func ticking() *moving {
	return &moving{at: time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)}
}

func TestAValueIsKeptUntilItStopsBeingWorthKeeping(t *testing.T) {
	clock := ticking()
	memory := cache.Recalling[string](8, clock.now)

	memory.Remember("film:603", "tmdb:603", "The Matrix", time.Hour)

	held, found := memory.Remembered("film:603")
	if !found || held != "The Matrix" {
		t.Fatalf("expected the value back, got %q and %v", held, found)
	}

	clock.past(61 * time.Minute)

	if _, still := memory.Remembered("film:603"); still {
		t.Fatal("expected the value to have stopped being worth keeping")
	}
}

func TestTheLeastRecentlyUsedGoesFirst(t *testing.T) {
	// Least recently *used*, which is what makes a read count as use: the
	// handful being looked at all afternoon stay, and the one somebody opened
	// once does not.
	clock := ticking()
	memory := cache.Recalling[int](2, clock.now)

	memory.Remember("first", "", 1, time.Hour)
	memory.Remember("second", "", 2, time.Hour)
	if _, found := memory.Remembered("first"); !found {
		t.Fatal("expected the first still to be there to be used")
	}

	memory.Remember("third", "", 3, time.Hour)

	if _, evicted := memory.Remembered("second"); evicted {
		t.Fatal("expected the one that was not used to have gone")
	}
	for _, key := range []string{"first", "third"} {
		if _, found := memory.Remembered(key); !found {
			t.Fatalf("expected %q to have stayed", key)
		}
	}
}

func TestEverythingAboutOneSubjectIsForgottenAtOnce(t *testing.T) {
	// What somebody asking for a thing to be looked up again means: not "drop
	// these four keys" but "find out about this thing again".
	clock := ticking()
	memory := cache.Recalling[string](8, clock.now)
	memory.Remember("scores:603", "tmdb:603", "83%", time.Hour)
	memory.Remember("editions:603", "tmdb:603", "4K", time.Hour)
	memory.Remember("scores:604", "tmdb:604", "73%", time.Hour)

	memory.Forget("tmdb:603")

	for _, key := range []string{"scores:603", "editions:603"} {
		if _, still := memory.Remembered(key); still {
			t.Fatalf("expected %q to have been forgotten", key)
		}
	}
	if _, gone := memory.Remembered("scores:604"); !gone {
		t.Fatal("expected what is kept about another subject to stay")
	}
	if memory.Held() != 1 {
		t.Fatalf("expected one value left, got %d", memory.Held())
	}
}

func TestAFilingWithNoLifetimeIsRefusedRatherThanKeptForever(t *testing.T) {
	store := cache.Holding(8, ticking().now)

	err := store.Keep(context.Background(), cache.Filing{Key: "film:603", Entity: []byte("x")})

	if err == nil {
		t.Fatal("expected a filing with no lifetime to be refused")
	}
	if store.Held() != 0 {
		t.Fatalf("expected nothing kept, got %d", store.Held())
	}
}

func TestAMissIsAnAnswerAndNotAFailure(t *testing.T) {
	// The ordinary state of a key nobody has asked for yet: a store that
	// failed on a miss would make every first request an error to handle.
	store := cache.Holding(8, ticking().now)

	kept, err := store.Kept(context.Background(), "film:nobody-asked")

	if err != nil {
		t.Fatalf("expected a miss to be an answer, got %v", err)
	}
	if kept.Found || len(kept.Entity) != 0 {
		t.Fatalf("expected nothing, got %+v", kept)
	}
}

func TestForgettingWhatWasNeverKeptIsNotAFailure(t *testing.T) {
	store := cache.Holding(8, ticking().now)

	if err := store.Forget(context.Background(), "tmdb:999"); err != nil {
		t.Fatalf("expected forgetting nothing to be no failure, got %v", err)
	}
}
