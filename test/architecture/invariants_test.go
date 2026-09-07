package architecture

import (
	"strings"
	"testing"
)

// The dependency rule points inward. capability defines the ports and depends
// on nothing; outcome models a terminated effect; lifetime owns fibers and
// resources; runtime interprets. Platform and test adapters sit outside all of
// it, and nothing in the core may reach an adapter.
var forbiddenImports = map[string][]string{
	"capability": {
		"effect-golang/internal",
		"effect-golang\"",
	},
	"internal/outcome": {
		"effect-golang/internal/lifetime",
		"effect-golang/internal/runtime",
		"effect-golang/internal/platform",
		"effect-golang\"",
	},
	"internal/lifetime": {
		"effect-golang/internal/runtime",
		"effect-golang/internal/platform",
		"effect-golang\"",
	},
	"internal/runtime": {
		"effect-golang/internal/platform",
		"effect-golang\"",
	},
	"internal/platform": {
		"effect-golang/internal/outcome",
		"effect-golang/internal/lifetime",
		"effect-golang/internal/runtime",
		"effect-golang\"",
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
const compositionRoot = "runtime.go"

func TestOnlyTheCompositionRootNamesLiveAdapters(t *testing.T) {
	for _, path := range sourcesIn(t, ".") {
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
const erasureBoundary = "erasure.go"

func TestErasedValuesStayInsideTheirBoundary(t *testing.T) {
	for _, path := range sourcesIn(t, ".") {
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

	directories := []string{".", "capability", "internal/outcome", "internal/lifetime",
		"internal/runtime", "internal/platform", "effecttest"}
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
