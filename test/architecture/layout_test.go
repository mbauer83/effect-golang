// Package architecture checks the invariants that hold over the shape of the
// code rather than over its behaviour: which package may import which, where
// erased values are allowed to appear, how large a source file may grow, and
// whether the documentation index reaches every document.
//
// These are the claims a reviewer would otherwise have to take on trust, so
// they are asserted instead.
package architecture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// moduleRoot walks up from the test's directory until it finds go.mod, so these
// checks read the whole module regardless of where the test binary runs.
func moduleRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("architecture: no go.mod above the test directory")
		}
		directory = parent
	}
}

// sourcesIn returns the non-test Go files directly inside one package
// directory, which is the unit these invariants are stated over.
func sourcesIn(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(moduleRoot(t), directory))
	if err != nil {
		t.Fatal(err)
	}

	sources := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		sources = append(sources, filepath.Join(moduleRoot(t), directory, name))
	}
	return sources
}

func readSource(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

// display shortens a path for a failure message.
func display(t *testing.T, path string) string {
	t.Helper()
	relative, err := filepath.Rel(moduleRoot(t), path)
	if err != nil {
		return path
	}
	return relative
}
