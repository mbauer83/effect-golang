package config

// The two shapes a flat source spells that a description cannot know in
// advance: a group of keys to enumerate, and several values held in one.

import (
	"strings"
)

// Table reads one entry per key beneath a name.
//
// The source knows the keys here, which is the whole difference from Nested:
// Nested moves a description deeper into paths the program named, and this asks
// the source which keys exist beneath a name and reads the entry description
// once per key. Limits per queue, credentials per tenant, a rate per route --
// adding one is a deployment change rather than a release.
//
// The entry description is read beneath each key, so a nameless primitive reads
// the key's own value and a named one reads a field of it.
//
//	config.Table("LIMITS", config.Int(""))        // LIMITS_READ, LIMITS_WRITE
//	config.Table("QUEUES", config.Int("DEPTH"))   // QUEUES_JOBS_DEPTH
//
// No keys beneath the name is an empty table and not a failure: a program with
// no tenants configured has no tenants, which is a thing it may legitimately
// be told.
func Table[A any](name string, of Config[A]) Config[map[string]A] {
	held := of.reader()
	return Config[map[string]A]{
		expects: []Expectation{{
			Path: pathOf(name),
			Type: "a table of " + typesIn(of.expects),
			// A table never fails for being absent: no keys beneath the name
			// is an empty table, so nothing has to be supplied.
			Optional: true,
		}},
		read: func(at reading) (map[string]A, Error) {
			here := at.under(name)
			children, err := here.source.Children(here.ctx, here.path)
			if err != nil {
				return nil, Unavailable(err, here.path...)
			}
			entries := make(map[string]A, len(children))
			failure := Error{}
			for _, child := range children {
				value, refused := held(here.under(child))
				failure = failure.And(refused)
				if refused.IsEmpty() {
					entries[child] = value
				}
			}
			if !failure.IsEmpty() {
				return nil, failure
			}
			return entries, Error{}
		},
	}
}

// Many reads several values from one key, separated.
//
// How a flat source spells a list: HOSTS=a,b,c. Each piece is read by the
// entry description, so the parsing and the refusal message are the ones that
// description already carries -- a piece that is not a port says so, and says
// which key it was in.
//
// The entry description reads the value where it stands, so it is a nameless
// primitive: config.Many("ports", ",", config.Port("")). A named one would
// have no key to read, because a piece of text has no keys beneath it.
func Many[A any](name string, separator string, of Config[A]) Config[[]A] {
	held := of.reader()
	return Config[[]A]{
		expects: []Expectation{{
			Path: pathOf(name),
			Type: typesIn(of.expects) + ` separated by "` + separator + `"`,
		}},
		read: func(at reading) ([]A, Error) {
			var missing []A
			here := at.under(name)
			raw, found, err := here.source.Value(here.ctx, here.path)
			switch {
			case err != nil:
				return missing, Unavailable(err, here.path...)
			case !found:
				return missing, Missing(here.path...)
			}
			pieces := strings.Split(raw, separator)
			values := make([]A, 0, len(pieces))
			failure := Error{}
			for _, piece := range pieces {
				value, refused := held(reading{
					ctx:    here.ctx,
					source: one(strings.TrimSpace(piece)),
					path:   here.path,
				})
				failure = failure.And(refused)
				values = append(values, value)
			}
			if !failure.IsEmpty() {
				return missing, failure
			}
			return values, Error{}
		},
	}
}

// typesIn names what a composite reads, for a printed expectation.
func typesIn(expects []Expectation) string {
	if len(expects) == 0 {
		return "values"
	}
	return expects[0].Type
}
