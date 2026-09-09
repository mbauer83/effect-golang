package cache

// A store in this process, which is what a single instance and every test
// needs.

import (
	"context"
	"time"
)

// Held is a Store in this process's own memory.
//
// The same LRU the typed face uses, holding bytes. Worth having for
// two reasons: a deployment of one instance needs no Redis to be correct, and
// a test of anything built on a Store needs no server at all.
type Held struct {
	held *LRU[[]byte]
}

// Holding is a store of at most this many values, on this clock.
func Holding(most int, now func() time.Time) *Held {
	return &Held{held: NewLRU[[]byte](most, now)}
}

// Get is what is held under a key, and whether anything is.
func (store *Held) Get(_ context.Context, key string) (Cached, error) {
	entity, found := store.held.Get(key)
	return Cached{Entity: entity, Found: found}, nil
}

// Put stores a value for as long as it is worth serving.
func (store *Held) Put(_ context.Context, entry Entry) error {
	if !entry.IsStorable() {
		return Fault{Doing: "keeping", Key: entry.Key, Err: ErrUnworthy}
	}
	store.held.Put(entry.Key, entry.About, entry.Entity, entry.Fresh)
	return nil
}

// Invalidate drops every entry about one subject.
func (store *Held) Invalidate(_ context.Context, subject string) error {
	store.held.Invalidate(subject)
	return nil
}

// Held is how many values are kept.
func (store *Held) Len() int { return store.held.Len() }
