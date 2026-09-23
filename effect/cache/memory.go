package cache

// A store in this process, which is what a single instance and every test
// needs.

import (
	"context"
	"time"
)

// MemoryStore is a Store in this process's own memory.
//
// The same LRU the typed face uses, holding bytes. Worth having for
// two reasons: a deployment of one instance needs no Redis to be correct, and
// a test of anything built on a Store needs no server at all.
type MemoryStore struct {
	entries *LRU[[]byte]
}

// NewMemoryStore is a store of at most this many values, on this clock.
func NewMemoryStore(most int, now func() time.Time) *MemoryStore {
	return &MemoryStore{entries: NewLRU[[]byte](most, now)}
}

// Get is what is held under a key, and whether anything is.
func (store *MemoryStore) Get(_ context.Context, key string) (Lookup, error) {
	entity, found := store.entries.Get(key)
	return Lookup{Entity: entity, Found: found}, nil
}

// Put stores a value for as long as it is worth serving.
func (store *MemoryStore) Put(_ context.Context, entry Entry) error {
	if !entry.IsStorable() {
		return Fault{Op: "write", Key: entry.Key, Err: ErrUnworthy}
	}
	store.entries.Put(entry.Key, entry.About, entry.Entity, entry.Fresh)
	return nil
}

// Invalidate drops every entry about one subject.
func (store *MemoryStore) Invalidate(_ context.Context, subject string) error {
	store.entries.Invalidate(subject)
	return nil
}

// Len is how many values are kept.
func (store *MemoryStore) Len() int { return store.entries.Len() }
