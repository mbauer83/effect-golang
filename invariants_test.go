package effect_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	effect "github.com/mbauer83/effect-golang"
	"github.com/mbauer83/effect-golang/effecttest"
)

// The invariants in this file are structural rather than behavioural, so they
// are checked directly instead of being left to review.

func TestChildrenTerminateBeforeScopeResourcesAreReleased(t *testing.T) {
	// The child uses a resource its own scope owns. Closing the scope must
	// cancel and await the child before releasing what the child was using;
	// releasing first would hand a live fiber a dead resource.
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}
	work := newBlocker(tracker)

	program := effect.Scoped(func(scope effect.Scope) forkedProgram {
		return acquire(tracker, scope, "shared").FlatMap(func(string) forkedProgram {
			return operations.Fork(work.program()).FlatMap(func(forkedFiber) forkedProgram {
				work.awaitStart()
				return operations.Succeed("body finished")
			})
		})
	})

	if exit := effect.Run(context.Background(), effect.Unit{}, program); exit.IsFailure() {
		t.Fatalf("unexpected failure: %v", exit)
	}

	events := tracker.Events()
	interrupted := slices.Index(events, "interrupted")
	released := slices.Index(events, "release shared")
	if interrupted < 0 || released < 0 {
		t.Fatalf("expected both a child interruption and a release, got %v", events)
	}
	if interrupted > released {
		t.Fatalf("expected the child to terminate before its scope's resource was released, got %v", events)
	}
}

// runtimePackages are the packages that must not reach for context values. The
// examples and tests are ordinary application code and are exempt.
var runtimePackages = []string{".", "capability", "internal/runtime", "internal/platform", "effecttest"}

func TestRuntimeMetadataIsNotHiddenInContextValues(t *testing.T) {
	// Runtime services, names, annotations, spans and scopes travel through an
	// explicit state parameter. Smuggling them through context.Value would make
	// them invisible in signatures and unavailable to the type checker.
	// Narrow patterns on purpose: Exit.Value and Either.RightValue are
	// unrelated, and a check that fires on them would be turned off.
	forbidden := []string{"context.WithValue", "ctx.Value(", "Context.Value("}

	for _, directory := range runtimePackages {
		for _, path := range runtimeSources(t, directory) {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, pattern := range forbidden {
				if strings.Contains(string(content), pattern) {
					t.Errorf("%s uses %q; runtime state must travel explicitly", path, pattern)
				}
			}
		}
	}
}

func TestErasedValuesStayInsideTheirBoundary(t *testing.T) {
	// Go cannot express a heterogeneous continuation stack, so the interpreter
	// carries values as any. That is confined to the internal runtime package
	// and to the one public file that documents the lifting, and it is confined
	// by this test rather than by good intentions.
	exempt := map[string]bool{"erasure.go": true}

	for _, path := range runtimeSources(t, ".") {
		if exempt[filepath.Base(path)] {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(content), "\n") {
			code, _, _ := strings.Cut(line, "//")
			if strings.Contains(code, "any)") || strings.Contains(code, "any{") || strings.Contains(code, "any\n") {
				t.Errorf("%s mentions an erased value outside erasure.go: %s", path, strings.TrimSpace(line))
			}
		}
	}
}

func TestRuntimePackagesDependOnlyInward(t *testing.T) {
	// The dependency rule points inward: platform adapters and test adapters
	// may reach the core, the core may reach its ports, and nothing in the core
	// may reach an adapter.
	forbidden := map[string][]string{
		"capability":        {"effect-golang/internal", "effect-golang\""},
		"internal/runtime":  {"effect-golang/internal/platform", "effect-golang\""},
		"internal/platform": {"effect-golang/internal/runtime"},
	}

	for directory, banned := range forbidden {
		for _, path := range runtimeSources(t, directory) {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, importPath := range banned {
				if strings.Contains(string(content), importPath) {
					t.Errorf("%s imports %q, which points outward", path, importPath)
				}
			}
		}
	}
}

func runtimeSources(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}

	sources := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		sources = append(sources, filepath.Join(directory, name))
	}
	return sources
}

func TestEveryPublicDocumentIsLinkedFromTheReadme(t *testing.T) {
	// Documentation the index does not reach is documentation nobody reads.
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}

	walk := func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		if !strings.Contains(string(readme), filepath.ToSlash(path)) {
			t.Errorf("%s is not linked from README.md", path)
		}
		return nil
	}
	if err := filepath.WalkDir("docs", walk); err != nil {
		t.Fatal(err)
	}
}
