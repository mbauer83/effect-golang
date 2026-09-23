// Package cache keeps values for a while, so a program does not compute or
// fetch the same answer twice.
//
// Two faces of one idea, because two different things are wanted from it.
// Within one process a caller wants the value it already had, typed, with no
// encoding and no failure to handle -- NewLRU is that. Between processes a
// caller wants an answer some other instance already got, which means bytes,
// a network, and something that can go wrong -- Store is that, and it is the
// port Redis, Valkey, a file or a table sits behind.
//
// Not a runtime capability, and the difference from config is worth stating: a
// program has one clock, one logger and one place its settings come from, and
// capability.Set holds one of each. It has as many caches as it has things
// worth keeping -- a bounded one in front of a shared one, a long-lived one
// for records and a short-lived one for listings -- so a cache is a port a
// program holds, like a repository, rather than something the runtime hands it.
//
// What is deliberately absent is a policy for filling one. Loading on a miss,
// collapsing concurrent misses into one load, refreshing ahead of expiry: each
// of those is a decision about the thing being cached, and a caller that has
// the miss in front of it can express any of them with the combinators it
// already has.
package cache

import (
	"context"
	"time"
)

// Entry is a value to be kept: its key, what it is about, what it holds, and
// how long that is worth keeping.
//
// The subject is what makes a cache clearable by somebody who does not know
// what is in it. A person asking a page to be read again means "find out about
// this thing again", not "drop these four keys" -- so what is kept says what
// it is about, and everything about one thing can be dropped together.
type Entry struct {
	Key    string
	About  string
	Entity []byte
	Fresh  time.Duration
}

// IsStorable reports whether this is an entry at all: a key to store it
// under, and a lifetime worth storing it for.
func (entry Entry) IsStorable() bool {
	return entry.Key != "" && entry.Fresh > 0
}

// Kept is what a store had, and whether it had anything still worth having.
//
// A value carrying whether there was one, rather than an absence reported as a
// failure, because a miss is the ordinary state of a key nobody has asked for
// yet. A store that failed on a miss would make every first request an error
// to handle.
type Lookup struct {
	Entity []byte
	Found  bool
}

// Store is where values are kept between processes.
//
// Context, bytes and error: the plainest shape an adapter can implement, and
// the same choice capability.ConfigSource makes for the same reason. Nothing
// in an adapter has to know about effects, and everything effectful is in the
// three functions in this package that wrap one.
type Store interface {
	// Get is what is held under a key, and whether anything is.
	Get(ctx context.Context, key string) (Lookup, error)
	// Put stores a value for as long as it is worth serving, under what it is
	// about as well as under its own key.
	Put(ctx context.Context, entry Entry) error
	// Invalidate drops every entry about one subject.
	//
	// A subject nothing was kept about is not a failure: somebody asking for
	// a thing nobody has read yet to be read again is asking for something
	// reasonable, and the answer is that there was nothing to drop.
	Invalidate(ctx context.Context, subject string) error
}

// Fault is why a store could not answer.
type Fault struct {
	// Op is what was being done, so a message says which of the three
	// failed without the caller adding it.
	Op  string
	Key string
	Err error
}

func (fault Fault) Error() string {
	message := "cache: " + fault.Op
	if fault.Key != "" {
		message += " " + fault.Key
	}
	if fault.Err != nil {
		message += ": " + fault.Err.Error()
	}
	return message
}

// Unwrap keeps the store's own error reachable, so a caller can tell a
// connection that is down from a value that would not encode.
func (fault Fault) Unwrap() error { return fault.Err }
