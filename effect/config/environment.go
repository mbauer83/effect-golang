package config

// The environment, which is the one place every deployment can put a setting.

import (
	"context"
	"os"
	"strings"
)

// Environment reads the process environment.
//
// A path is spelled in upper case with underscores between its segments, which
// is the convention every deployment tool already writes: db.host is DB_HOST,
// limits.read is LIMITS_READ. A segment already spelled that way is unchanged,
// so a description written in the environment's own spelling reads the same.
//
// It is read once, when this is constructed, rather than per lookup. A
// program's environment does not change under it -- Go's own os.Setenv is not
// safe beside a running program that reads it -- and reading a snapshot means
// two descriptions read at different moments cannot disagree about what the
// deployment said.
func Environment() Source {
	return environmentOf(os.Environ())
}

// EnvironmentOf reads a fixed environment in the environment's own spelling,
// for a test that wants the naming without the process.
func EnvironmentOf(entries ...string) Source {
	return environmentOf(entries)
}

func environmentOf(entries []string) Source {
	values := make(map[string]string, len(entries))
	for _, entry := range entries {
		name, value, found := strings.Cut(entry, "=")
		if found {
			values[name] = value
		}
	}
	return environment{values: values, keys: keysOf(values)}
}

type environment struct {
	values map[string]string
	keys   []string
}

func (source environment) Value(_ context.Context, path []string) (string, bool, error) {
	value, found := source.values[shouted(path)]
	return value, found, nil
}

func (source environment) Children(_ context.Context, path []string) ([]string, error) {
	return lowered(childrenOf(source.keys, shouted(path), "_")), nil
}

// shouted spells a path the way the environment does.
func shouted(path []string) string {
	segments := make([]string, 0, len(path))
	for _, segment := range path {
		segments = append(segments, strings.ToUpper(segment))
	}
	return strings.Join(segments, "_")
}

// lowered gives a table's keys back in the spelling a program reads them in.
//
// A description that enumerates LIMITS gets read and write, not READ and
// WRITE: the shouting is the environment's convention, and a map a program
// indexes should not carry it.
func lowered(children []string) []string {
	named := make([]string, 0, len(children))
	for _, child := range children {
		named = append(named, strings.ToLower(child))
	}
	return named
}
