package config

// Where values come from, and how several places become one.
//
// This is the n side of the relation. A deployment has more than one place a
// setting can come from -- a mounted file, the environment, a secret store, the
// defaults a chart shipped -- and a program should not know which one answered.
// Sources composes them into a single source that a description reads once.

import (
	"context"
	"slices"
	"strings"
)

// Fixed is a source over values a program already holds.
//
// What a test configures a runtime with, and what a program that has parsed
// its own flags or a document into a flat map hands over. Paths are spelled
// with dots: "db.host", "limits.read".
func Fixed(values map[string]string) Source {
	mapEntry := make(map[string]string, len(values))
	for key, value := range values {
		mapEntry[key] = value
	}
	return fixed{values: mapEntry}
}

type fixed struct {
	values map[string]string
}

func (source fixed) Value(_ context.Context, path []string) (string, bool, error) {
	value, found := source.values[Render(path)]
	return value, found, nil
}

func (source fixed) Children(_ context.Context, path []string) ([]string, error) {
	return childrenOf(keysOf(source.values), Render(path), "."), nil
}

// Sources reads from each in turn, and the first that carries a path answers.
//
// Order is precedence, so the one a deployment overrides with comes first:
//
//	config.Sources(config.Environment(), config.Fixed(defaults))
//
// A source that cannot be consulted is not a source that does not carry the
// path. It stops the search and is reported, because falling through from a
// secret store that is down to the defaults beneath it would start a program
// with settings nobody chose -- silently, and only when the store is down.
//
// Children are gathered from every source, in order and without repeats, so a
// table whose entries are spread across a file and the environment is one
// table.
func Sources(sources ...Source) Source {
	makeed := make([]Source, 0, len(sources))
	for _, source := range sources {
		if source != nil {
			makeed = append(makeed, source)
		}
	}
	if len(makeed) == 1 {
		return makeed[0]
	}
	return fallback{sources: makeed}
}

type fallback struct {
	sources []Source
}

func (source fallback) Value(ctx context.Context, path []string) (string, bool, error) {
	for _, heldValue := range source.sources {
		value, found, err := heldValue.Value(ctx, path)
		if err != nil {
			return "", false, err
		}
		if found {
			return value, true, nil
		}
	}
	return "", false, nil
}

func (source fallback) Children(ctx context.Context, path []string) ([]string, error) {
	gathered := []string{}
	seen := map[string]bool{}
	for _, heldValue := range source.sources {
		children, err := heldValue.Children(ctx, path)
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			if !seen[child] {
				seen[child] = true
				gathered = append(gathered, child)
			}
		}
	}
	return gathered, nil
}

// Beneath reads a source as though the description started at a path inside
// it.
//
// What mounts one service's settings inside a document that holds several:
// the program describes what it needs, and this says where in the file that is.
func Beneath(source Source, path ...string) Source {
	if source == nil || len(path) == 0 {
		return source
	}
	return moved{source: source, prefix: slices.Clone(path)}
}

type moved struct {
	source Source
	prefix []string
}

func (source moved) Value(ctx context.Context, path []string) (string, bool, error) {
	return source.source.Value(ctx, source.at(path))
}

func (source moved) Children(ctx context.Context, path []string) ([]string, error) {
	return source.source.Children(ctx, source.at(path))
}

func (source moved) at(path []string) []string {
	return append(slices.Clone(source.prefix), path...)
}

// Renaming spells each segment of a path the way one source spells it.
//
// The adapter between a description's names and a source's conventions: a
// program that describes db.maxConnections reads it from a file spelled that
// way, and from an environment spelled DB_MAX_CONNECTIONS, without saying so
// twice.
func Renaming(source Source, spell func(string) string) Source {
	if source == nil || spell == nil {
		return source
	}
	return renamed{source: source, spell: spell}
}

type renamed struct {
	source Source
	spell  func(string) string
}

func (source renamed) Value(ctx context.Context, path []string) (string, bool, error) {
	return source.source.Value(ctx, source.renamePath(path))
}

func (source renamed) Children(ctx context.Context, path []string) ([]string, error) {
	return source.source.Children(ctx, source.renamePath(path))
}

func (source renamed) renamePath(path []string) []string {
	makeed := make([]string, 0, len(path))
	for _, segment := range path {
		makeed = append(makeed, source.spell(segment))
	}
	return makeed
}

// one is a source over a single piece of text, which is how a separated list
// hands each of its pieces to the description that reads them.
func one(value string) Source {
	return single{value: value}
}

type single struct {
	value string
}

func (source single) Value(context.Context, []string) (string, bool, error) {
	return source.value, true, nil
}

func (single) Children(context.Context, []string) ([]string, error) {
	return nil, nil
}

// childrenOf names the segments directly beneath a prefix, in the order the
// keys were given and without repeats.
//
// Shared by every flat source: the environment separates its segments with an
// underscore and a document with a dot, and the arithmetic is otherwise the
// same.
func childrenOf(keys []string, prefix string, separator string) []string {
	if prefix != "" {
		prefix += separator
	}
	children := []string{}
	seen := map[string]bool{}
	for _, key := range keys {
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			continue
		}
		beneath := strings.TrimPrefix(key, prefix)
		if beneath == "" {
			continue
		}
		child, _, _ := strings.Cut(beneath, separator)
		if !seen[child] {
			seen[child] = true
			children = append(children, child)
		}
	}
	return children
}

// keysOf reads a map's keys in a stable order, so a table's entries arrive the
// same way twice.
func keysOf(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
