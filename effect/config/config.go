package config

import (
	"context"
	"errors"
	"slices"

	"github.com/mbauer83/effect-golang/effect/capability"
)

// Source is the port a description reads through.
type Source = capability.ConfigSource

// Config describes one value a program is configured with: where it is read
// from, how the text becomes an A, what happens when nobody supplied it, and
// what to print when somebody asks what this program needs.
//
// A description and not a read. It is built once, holds no source and no
// value, and stays usable by any number of readers against any number of
// sources -- which is what lets one program's settings be assembled at
// start-up, checked in a test against a fixed map, and printed by a
// documentation command, all from the one description.
//
// The zero value describes nothing and fails when read, so a Config that was
// never constructed cannot be mistaken for one that found nothing.
type Config[A any] struct {
	read    func(at reading) (A, Error)
	expects []Expectation
}

// Expectation is one value a description asks for, in a form that can be
// printed.
//
// A description that can only be read is a description a program cannot
// explain. This is what makes "what does this service need to be told" a
// question the code answers rather than a page somebody maintains beside it.
type Expectation struct {
	// Path is where the value is read from, as the description names it. A
	// source may spell it differently -- the environment spells db.host as
	// DB_HOST -- and that is the source's business.
	Path []string
	// Type is what the text is read as: "text", "integer", "duration".
	Type string
	// Doc is what Documented said about it, or empty.
	Doc string
	// Default is the stand-in rendered as text, and empty when there is none.
	Default string
	// Optional is true when absence is an answer this description accepts.
	Optional bool
	// Secret is true when the value must not be printed once read.
	Secret bool
}

// reading is one read in progress: where the values come from, and where in
// the description the reader currently is.
type reading struct {
	ctx    context.Context
	source Source
	path   []string
}

// under descends into a named key. An empty name stays where it is, which is
// what makes a nameless primitive read the value at the current path -- the
// entry of a table, or one piece of a separated list.
func (at reading) under(name string) reading {
	if name == "" {
		return at
	}
	at.path = append(slices.Clone(at.path), name)
	return at
}

// Read reads a description from a source.
//
// The whole of reading: everything else in this package builds descriptions.
// The runtime calls this with the source its capabilities carry, which is why
// a program's settings need no argument passed down to whoever wants them.
func Read[A any](ctx context.Context, source Source, description Config[A]) (A, Error) {
	if source == nil {
		var missing A
		return missing, Unavailable(errNoSource)
	}
	return description.reader()(reading{ctx: ctx, source: source})
}

var errNoSource = errors.New("config: no source to read from")

// errZeroDescription is what a description nobody constructed reports.
//
// A Config is only built by the constructors here, so the zero value is a
// declared variable somebody never assigned. It fails rather than succeeding
// with a zero value, and it fails rather than panicking, so one forgotten
// field in a composite is reported beside whatever else was wrong.
var errZeroDescription = Invalid("described; the zero Config describes nothing")

// reader is this description's read, total on a description nobody built.
//
// Every combinator goes through it, so a zero Config nested, mapped or made a
// field of is reported where it is read instead of being a nil call at the
// bottom of a stack.
func (description Config[A]) reader() func(reading) (A, Error) {
	if description.read != nil {
		return description.read
	}
	return func(reading) (A, Error) {
		var missing A
		return missing, errZeroDescription
	}
}

// Expects are the values this description asks for, in the order it asks.
func (description Config[A]) Expects() []Expectation {
	return slices.Clone(description.expects)
}

// Map transforms a value that was read.
func (description Config[A]) Map[B any](transform func(A) B) Config[B] {
	held := description.reader()
	return Config[B]{
		expects: description.expects,
		read: func(at reading) (B, Error) {
			value, failure := held(at)
			if !failure.IsEmpty() {
				var missing B
				return missing, failure
			}
			return transform(value), Error{}
		},
	}
}

// MapOrFail transforms a value that was read, with a transform that may refuse
// it.
//
// The refusal is reported against the path the value came from, as a value the
// source held and the description could not use -- which is what it is. Reading
// a port and then finding it outside the range a listener accepts is the same
// kind of mistake as reading "eight" where a number was wanted.
func (description Config[A]) MapOrFail[B any](transform func(A) (B, error)) Config[B] {
	held := description.reader()
	subject := description.subject()
	return Config[B]{
		expects: description.expects,
		read: func(at reading) (B, Error) {
			var missing B
			value, failure := held(at)
			if !failure.IsEmpty() {
				return missing, failure
			}
			transformed, err := transform(value)
			if err != nil {
				return missing, Invalid(err.Error(),
					append(slices.Clone(at.path), subject...)...)
			}
			return transformed, Error{}
		},
	}
}

// Validated keeps a value only when it satisfies a predicate, and otherwise
// reports it as a value this description cannot use.
//
//	config.Int("PORT").Validated("a port above 1024", func(port int) bool {
//	    return port > 1024
//	})
//
// The message says what was wanted rather than what was wrong, because it is
// read beside the value that failed it.
func (description Config[A]) Validated(message string, keep func(A) bool) Config[A] {
	return description.MapOrFail(func(value A) (A, error) {
		if !keep(value) {
			return value, errors.New(message)
		}
		return value, nil
	})
}

// Documented says what a value is for, which is what a program prints when it
// is asked what it needs.
//
// It applies to every expectation the description carries that has nothing
// said about it yet, so documenting a composite documents its parts and
// documenting a part keeps what it already said.
func (description Config[A]) Documented(doc string) Config[A] {
	described := slices.Clone(description.expects)
	for at := range described {
		if described[at].Doc == "" {
			described[at].Doc = doc
		}
	}
	description.expects = described
	return description
}

// subject is the path of the first value this description reads, relative to
// wherever it is read. What a transform's refusal is reported against: a
// description of one value has exactly one, and a composite's first is the one
// a reader is looking at when the composite is named.
func (description Config[A]) subject() []string {
	if len(description.expects) == 0 {
		return nil
	}
	return description.expects[0].Path
}

// nestedExpectations returns expectations with name in front of each path.
func nestedExpectations(name string, held []Expectation) []Expectation {
	if name == "" {
		return held
	}
	moved := make([]Expectation, 0, len(held))
	for _, expectation := range held {
		expectation.Path = append([]string{name}, expectation.Path...)
		moved = append(moved, expectation)
	}
	return moved
}
