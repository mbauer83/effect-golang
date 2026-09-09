// Package config describes what a program must be told before it will run,
// and reads it.
//
// A Config[A] is a description and not a read: it says which paths a value
// comes from, how the text at them becomes an A, what is optional and what a
// default is. Reading it is an effect, so a program's settings arrive through
// the same failure channel, the same interruption and the same layers as
// everything else it does.
package config

import (
	"slices"
	"strings"
)

// Kind classifies one node of an Error.
type Kind string

const (
	// KindEmpty is the identity of And and Or: no failure.
	KindEmpty Kind = ""
	// KindMissing is a path the source does not carry.
	KindMissing Kind = "missing"
	// KindInvalid is a value the description could not read as its type.
	KindInvalid Kind = "invalid"
	// KindUnavailable is a source that could not be consulted.
	KindUnavailable Kind = "unavailable"
	// KindAnd composes failures that a description needed both of.
	KindAnd Kind = "and"
	// KindOr composes the failures of alternatives that were all tried.
	KindOr Kind = "or"
)

// Error is why a description could not be read.
//
// Compositional, because a description is: a program wanting a host and a port
// and given neither has two failures, and reporting one of them sends whoever
// is deploying it round the loop twice. And keeps both, Or keeps every
// alternative that was tried, and Failures flattens the tree for printing.
//
// Its zero value is the empty error, which is the identity of And and Or and
// means no failure. That is the same contract Cause has in the runtime: a
// reader holding an Error asks IsEmpty rather than comparing against nil.
type Error struct {
	node *node
}

// node is one node of the tree. Unexported and immutable: the constructors in
// this file are its only writers, so an Error a caller holds cannot be edited
// into one that says something else.
type node struct {
	kind    Kind
	path    []string
	message string
	err     error
	left    *node
	right   *node
}

// Failure is one leaf of an Error: a single thing that was wrong.
type Failure struct {
	Kind    Kind
	Path    []string
	Message string
	// Err is the source's own error, and nil unless Kind is KindUnavailable.
	Err error
}

// Missing reports a path the source does not carry.
func Missing(path ...string) Error {
	return Error{node: &node{kind: KindMissing, path: slices.Clone(path)}}
}

// Invalid reports a value that is present and unreadable as its type.
func Invalid(message string, path ...string) Error {
	return Error{node: &node{
		kind:    KindInvalid,
		path:    slices.Clone(path),
		message: message,
	}}
}

// Unavailable reports a source that could not be consulted.
//
// Distinct from Missing, and the distinction is load-bearing: a default stands
// in for a value nobody supplied, and standing in for a secret store that is
// down would start a program with settings it was never given.
func Unavailable(err error, path ...string) Error {
	return Error{node: &node{
		kind: KindUnavailable,
		path: slices.Clone(path),
		err:  err,
	}}
}

// And composes two failures a description needed both sides of.
func (failure Error) And(that Error) Error {
	return failure.compose(KindAnd, that)
}

// Or composes the failures of alternatives that were all tried.
func (failure Error) Or(that Error) Error {
	return failure.compose(KindOr, that)
}

func (failure Error) compose(kind Kind, that Error) Error {
	if failure.IsEmpty() {
		return that
	}
	if that.IsEmpty() {
		return failure
	}
	return Error{node: &node{kind: kind, left: failure.node, right: that.node}}
}

// IsEmpty reports whether this is the identity: no failure.
func (failure Error) IsEmpty() bool {
	return failure.node == nil || failure.node.kind == KindEmpty
}

// Kind returns this node's category, and KindEmpty for the identity.
func (failure Error) Kind() Kind {
	if failure.node == nil {
		return KindEmpty
	}
	return failure.node.kind
}

// Failures flattens the tree into its leaves, left to right.
//
// The order a description was written in, which is the order somebody reading
// the output expects: the host before the port because that is how the program
// asked for them.
func (failure Error) Failures() []Failure {
	return appendLeaves(nil, failure.node)
}

func appendLeaves(into []Failure, failure *node) []Failure {
	if failure == nil {
		return into
	}
	switch failure.kind {
	case KindEmpty:
		return into
	case KindAnd, KindOr:
		return appendLeaves(appendLeaves(into, failure.left), failure.right)
	default:
		return append(into, Failure{
			Kind:    failure.kind,
			Path:    slices.Clone(failure.path),
			Message: failure.message,
			Err:     failure.err,
		})
	}
}

// MissingOnly reports whether every leaf is a path nobody supplied.
//
// What a default is allowed to stand in for. A value that was supplied and is
// unreadable, or a source that was down, is not absence: silently defaulting
// either is how a mistyped setting becomes a program running on numbers nobody
// chose. The empty error is not missing anything, so it answers false.
func (failure Error) MissingOnly() bool {
	leaves := failure.Failures()
	if len(leaves) == 0 {
		return false
	}
	for _, leaf := range leaves {
		if leaf.Kind != KindMissing {
			return false
		}
	}
	return true
}

// Prefixed returns this error with path in front of every leaf's path, which is
// how a nested description reports where it was reading.
func (failure Error) Prefixed(path ...string) Error {
	if failure.IsEmpty() || len(path) == 0 {
		return failure
	}
	return Error{node: withPrefix(failure.node, path)}
}

func withPrefix(failure *node, path []string) *node {
	if failure == nil {
		return nil
	}
	if failure.kind == KindAnd || failure.kind == KindOr {
		return &node{
			kind:  failure.kind,
			left:  withPrefix(failure.left, path),
			right: withPrefix(failure.right, path),
		}
	}
	moved := *failure
	moved.path = append(slices.Clone(path), failure.path...)
	return &moved
}

// Error renders the tree, keeping its shape: "and" for failures a description
// needed both of, "or" for alternatives that were all tried.
func (failure Error) Error() string {
	if failure.IsEmpty() {
		return "no configuration failure"
	}
	return render(failure.node)
}

func render(failure *node) string {
	switch failure.kind {
	case KindAnd:
		return render(failure.left) + " and " + render(failure.right)
	case KindOr:
		return render(failure.left) + " or " + render(failure.right)
	case KindMissing:
		return "no value at " + Render(failure.path)
	case KindInvalid:
		return Render(failure.path) + " is not " + failure.message
	case KindUnavailable:
		return Render(failure.path) + " could not be read: " + failure.err.Error()
	default:
		return "no configuration failure"
	}
}

// Render spells a path the way a description names it, which is not
// necessarily how a source spells it: a source that reads DB_HOST from the
// environment still reports the path the program asked for.
func Render(path []string) string {
	if len(path) == 0 {
		return "the root"
	}
	return strings.Join(path, ".")
}
