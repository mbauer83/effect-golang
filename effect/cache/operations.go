package cache

// Reading and writing a store, as effects.
//
// The three functions a program actually calls. They are here rather than on
// the interface for the reason capability.ConfigSource makes the same choice:
// an adapter implements the plainest thing it can -- context, bytes, error --
// and everything about interpretation, cancellation and the failure channel is
// in one place that wraps it.

import (
	"context"
	"errors"

	"github.com/mbauer83/effect-golang/effect"
)

// ErrUnworthy is a entry with nothing to keep it under, or no time worth
// keeping it for.
var ErrUnworthy = errors.New("a entry has a key and a lifetime")

// Read is what a store has under a key.
//
// A miss is a Lookup saying so rather than a failure, so the ordinary shape of a
// caller is one branch on Found and not an error to recover from.
func Read[R any](store Store, key string) effect.Effect[R, Fault, Lookup] {
	return effect.Try(
		func(ctx context.Context, _ R) (Lookup, error) {
			return store.Get(ctx, key)
		},
		faultOf("read", key),
	).WithName("cache read")
}

// Write keeps a value for as long as it is worth keeping.
func Write[R any](store Store, entry Entry) effect.Effect[R, Fault, effect.Unit] {
	if !entry.IsStorable() {
		return effect.Fail[R, effect.Unit](Fault{
			Op: "write", Key: entry.Key, Err: ErrUnworthy,
		})
	}
	return effect.Try(
		func(ctx context.Context, _ R) (effect.Unit, error) {
			return effect.Unit{}, store.Put(ctx, entry)
		},
		faultOf("write", entry.Key),
	).WithName("cache write")
}

// Drop forgets everything kept about one subject, so that whatever reads it
// next finds out again.
func Drop[R any](store Store, about string) effect.Effect[R, Fault, effect.Unit] {
	return effect.Try(
		func(ctx context.Context, _ R) (effect.Unit, error) {
			return effect.Unit{}, store.Invalidate(ctx, about)
		},
		faultOf("drop", about),
	).WithName("cache drop")
}

// faultOf keeps a store's own error reachable rather than wrapping a Fault in
// a Fault, so a caller reading Op sees what actually failed.
func faultOf(op string, key string) func(error) Fault {
	return func(err error) Fault {
		var fault Fault
		if errors.As(err, &fault) {
			return fault
		}
		return Fault{Op: op, Key: key, Err: err}
	}
}
