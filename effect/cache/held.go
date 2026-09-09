package cache

// A store in this process, which is what a single instance and every test
// needs.

import (
	"context"
	"time"
)

// Held is a Store in this process's own memory.
//
// The same recollection the typed face uses, holding bytes. Worth having for
// two reasons: a deployment of one instance needs no Redis to be correct, and
// a test of anything built on a Store needs no server at all.
type Held struct {
	held *Recollection[[]byte]
}

// Holding is a store of at most this many values, on this clock.
func Holding(most int, now func() time.Time) *Held {
	return &Held{held: Recalling[[]byte](most, now)}
}

// Kept is what is held under a key, and whether anything is.
func (store *Held) Kept(_ context.Context, key string) (Kept, error) {
	entity, found := store.held.Remembered(key)
	return Kept{Entity: entity, Found: found}, nil
}

// Keep files a value for as long as it is worth keeping.
func (store *Held) Keep(_ context.Context, filing Filing) error {
	if !filing.IsWorthKeeping() {
		return Fault{Doing: "keeping", Key: filing.Key, Err: ErrUnworthy}
	}
	store.held.Remember(filing.Key, filing.About, filing.Entity, filing.Fresh)
	return nil
}

// Forget drops everything kept about one subject.
func (store *Held) Forget(_ context.Context, about string) error {
	store.held.Forget(about)
	return nil
}

// Held is how many values are kept.
func (store *Held) Held() int { return store.held.Held() }
