package architecture

import (
	"strings"
	"testing"
)

// The dependency rule points inward. capability defines the ports and depends
// on nothing; outcome models a terminated effect; lifetime owns fibers and
// resources; runtime interprets. Platform and test adapters sit outside all of
// it, and nothing in the core may reach an adapter.
//
// Nesting the internals under effect/ means Go itself already forbids anything
// outside the domain from importing them. These checks cover what Go cannot:
// the ordering among the internal layers.
var forbiddenImports = map[string][]string{
	"effect/capability": {
		"effect-golang/effect/internal",
		"effect-golang/effect\"",
	},
	"effect/internal/outcome": {
		"effect-golang/effect/internal/lifetime",
		"effect-golang/effect/internal/runtime",
		"effect-golang/effect/internal/platform",
		"effect-golang/effect\"",
	},
	"effect/internal/lifetime": {
		"effect-golang/effect/internal/runtime",
		"effect-golang/effect/internal/platform",
		"effect-golang/effect\"",
	},
	"effect/internal/runtime": {
		"effect-golang/effect/internal/platform",
		"effect-golang/effect\"",
	},
	"effect/internal/platform": {
		"effect-golang/effect/internal/outcome",
		"effect-golang/effect/internal/lifetime",
		"effect-golang/effect/internal/runtime",
		"effect-golang/effect\"",
	},
}

func TestPackagesDependOnlyInward(t *testing.T) {
	for directory, banned := range forbiddenImports {
		for _, path := range sourcesIn(t, directory) {
			source := readSource(t, path)
			for _, importPath := range banned {
				if strings.Contains(source, importPath) {
					t.Errorf("%s imports %q, which points outward", display(t, path), importPath)
				}
			}
		}
	}
}

// The composition root is the one place that may name a live adapter. Keeping
// it to a single file is what stops the domain from acquiring an infrastructure
// dependency one convenience at a time.
const compositionRoot = "effect/runtime.go"

func TestOnlyTheCompositionRootNamesLiveAdapters(t *testing.T) {
	for _, path := range sourcesIn(t, "effect") {
		if display(t, path) == compositionRoot {
			continue
		}
		if strings.Contains(readSource(t, path), "internal/platform") {
			t.Errorf("%s names a live adapter; wiring belongs in %s", display(t, path), compositionRoot)
		}
	}
}

// Go cannot express a heterogeneous continuation stack, so the interpreter
// carries values erased. That is confined to the internal packages and to the
// one public file that documents the lifting, and it is confined by this test
// rather than by good intentions.
const erasureBoundary = "effect/erasure.go"

func TestErasedValuesStayInsideTheirBoundary(t *testing.T) {
	for _, path := range sourcesIn(t, "effect") {
		if display(t, path) == erasureBoundary {
			continue
		}
		for _, line := range strings.Split(readSource(t, path), "\n") {
			code, _, _ := strings.Cut(line, "//")
			if strings.Contains(code, "any)") || strings.Contains(code, "any{") {
				t.Errorf("%s mentions an erased value outside %s: %s",
					display(t, path), erasureBoundary, strings.TrimSpace(line))
			}
		}
	}
}

// Runtime services, names, annotations, spans and scopes travel through an
// explicit state parameter. Smuggling them through context.Value would make
// them invisible in signatures and unavailable to the type checker.
func TestRuntimeMetadataIsNotHiddenInContextValues(t *testing.T) {
	// Narrow patterns on purpose: Exit.Value and Either.RightValue are
	// unrelated, and a check that fired on them would be turned off.
	forbidden := []string{"context.WithValue", "ctx.Value(", "Context.Value("}

	directories := []string{"effect", "effect/capability", "effect/internal/outcome",
		"effect/internal/lifetime", "effect/internal/runtime", "effect/internal/platform",
		"effecttest"}
	for _, directory := range directories {
		for _, path := range sourcesIn(t, directory) {
			source := readSource(t, path)
			for _, pattern := range forbidden {
				if strings.Contains(source, pattern) {
					t.Errorf("%s uses %q; runtime state must travel explicitly",
						display(t, path), pattern)
				}
			}
		}
	}
}

// TestModuleRootHoldsNoSource keeps the module root for project metadata and
// documentation. The domain package must live in one directory because Effect,
// Exit, Cause, Scope and Fiber share private representation, but that directory
// does not have to be the root, and a root full of source files is not a layout.
func TestModuleRootHoldsNoSource(t *testing.T) {
	if sources := sourcesIn(t, "."); len(sources) != 0 {
		for _, path := range sources {
			t.Errorf("%s sits in the module root; source belongs in a package directory", display(t, path))
		}
	}
}
