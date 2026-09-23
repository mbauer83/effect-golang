package unit

// What one process keeps to hand.

import (
	"context"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang/effect/cache"
)

// manualClock is a clock a test holds.
type manualClock struct {
	at time.Time
}

func (clock *manualClock) now() time.Time { return clock.at }

func (clock *manualClock) past(by time.Duration) { clock.at = clock.at.Add(by) }

func newManualClock() *manualClock {
	return &manualClock{at: time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)}
}

func TestAValueIsKeptUntilItStopsBeingWorthKeeping(t *testing.T) {
	clock := newManualClock()
	memory := cache.NewLRU[string](8, clock.now)

	memory.Put("film:603", "tmdb:603", "The Matrix", time.Hour)

	held, found := memory.Get("film:603")
	if !found || held != "The Matrix" {
		t.Fatalf("expected the value back, got %q and %v", held, found)
	}

	clock.past(61 * time.Minute)

	if _, still := memory.Get("film:603"); still {
		t.Fatal("expected the value to have stopped being worth keeping")
	}
}

func TestTheLeastRecentlyUsedGoesFirst(t *testing.T) {
	// Least recently *used*, which is what makes a read count as use: the
	// handful being looked at all afternoon stay, and the one somebody opened
	// once does not.
	clock := newManualClock()
	memory := cache.NewLRU[int](2, clock.now)

	memory.Put("first", "", 1, time.Hour)
	memory.Put("second", "", 2, time.Hour)
	if _, found := memory.Get("first"); !found {
		t.Fatal("expected the first still to be there to be used")
	}

	memory.Put("third", "", 3, time.Hour)

	if _, evicted := memory.Get("second"); evicted {
		t.Fatal("expected the one that was not used to have gone")
	}
	for _, key := range []string{"first", "third"} {
		if _, found := memory.Get(key); !found {
			t.Fatalf("expected %q to have stayed", key)
		}
	}
}

func TestEverythingAboutOneSubjectIsForgottenAtOnce(t *testing.T) {
	// What somebody asking for a thing to be looked up again means: not "drop
	// these four keys" but "find out about this thing again".
	clock := newManualClock()
	memory := cache.NewLRU[string](8, clock.now)
	memory.Put("scores:603", "tmdb:603", "83%", time.Hour)
	memory.Put("editions:603", "tmdb:603", "4K", time.Hour)
	memory.Put("scores:604", "tmdb:604", "73%", time.Hour)

	memory.Invalidate("tmdb:603")

	for _, key := range []string{"scores:603", "editions:603"} {
		if _, still := memory.Get(key); still {
			t.Fatalf("expected %q to have been forgotten", key)
		}
	}
	if _, gone := memory.Get("scores:604"); !gone {
		t.Fatal("expected what is kept about another subject to stay")
	}
	if memory.Len() != 1 {
		t.Fatalf("expected one value left, got %d", memory.Len())
	}
}

func TestAFilingWithNoLifetimeIsRefusedRatherThanKeptForever(t *testing.T) {
	store := cache.NewMemoryStore(8, newManualClock().now)

	err := store.Put(context.Background(), cache.Entry{Key: "film:603", Entity: []byte("x")})

	if err == nil {
		t.Fatal("expected a filing with no lifetime to be refused")
	}
	if store.Len() != 0 {
		t.Fatalf("expected nothing kept, got %d", store.Len())
	}
}

func TestAMissIsAnAnswerAndNotAFailure(t *testing.T) {
	// The ordinary state of a key nobody has asked for yet: a store that
	// failed on a miss would make every first request an error to handle.
	store := cache.NewMemoryStore(8, newManualClock().now)

	kept, err := store.Get(context.Background(), "film:nobody-asked")

	if err != nil {
		t.Fatalf("expected a miss to be an answer, got %v", err)
	}
	if kept.Found || len(kept.Entity) != 0 {
		t.Fatalf("expected nothing, got %+v", kept)
	}
}

func TestForgettingWhatWasNeverKeptIsNotAFailure(t *testing.T) {
	store := cache.NewMemoryStore(8, newManualClock().now)

	if err := store.Invalidate(context.Background(), "tmdb:999"); err != nil {
		t.Fatalf("expected forgetting nothing to be no failure, got %v", err)
	}
}
