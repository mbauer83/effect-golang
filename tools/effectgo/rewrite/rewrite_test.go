package rewrite_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/mbauer83/effect-golang/tools/effectgo/rewrite"
)

// The cases module is tested as written and as rewritten, and both must pass:
// the rewrite is an optimisation, so any difference between the two is a bug.
func TestTheCasesPassAsWrittenAndAsRewritten(t *testing.T) {
	dir, err := filepath.Abs("../testdata/cases")
	if err != nil {
		t.Fatal(err)
	}
	environment := append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Tests: true,
		Dir:   dir,
		Env:   environment,
	}, "./...")
	if err != nil {
		t.Fatal(err)
	}
	replace := map[string]string{}
	rewritten, declined := 0, 0
	for _, pkg := range loaded {
		for _, problem := range pkg.Errors {
			t.Fatalf("loading the cases: %v", problem)
		}
		for i, file := range pkg.Syntax {
			filename := pkg.CompiledGoFiles[i]
			if _, done := replace[filename]; done {
				continue
			}
			text, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			result := rewrite.File(pkg.Fset, file, pkg.TypesInfo, pkg.Types, text)
			rewritten += result.Rewrites
			declined += len(result.Declines)
			for _, decline := range result.Declines {
				t.Logf("declined %s: %s", decline.Position, decline.Reason)
			}
			if result.Source == nil {
				continue
			}
			target := filepath.Join(t.TempDir(), filepath.Base(filename))
			if err := os.WriteFile(target, result.Source, 0o644); err != nil {
				t.Fatal(err)
			}
			replace[filename] = target
		}
	}
	if declined != 1 {
		t.Errorf("expected exactly the range over a map to be declined, got %d declined", declined)
	}
	if rewritten < 20 {
		t.Errorf("expected every other body rewritten, got %d", rewritten)
	}
	encoded, _ := json.Marshal(map[string]map[string]string{"Replace": replace})
	overlay := filepath.Join(t.TempDir(), "overlay.json")
	if err := os.WriteFile(overlay, encoded, 0o644); err != nil {
		t.Fatal(err)
	}

	for name, args := range map[string][]string{
		"as written":   {"test", "-count=1", "./..."},
		"as rewritten": {"test", "-count=1", "-overlay=" + overlay, "./..."},
		"vet":          {"vet", "-overlay=" + overlay, "./..."},
	} {
		command := exec.Command("go", args...)
		command.Dir, command.Env = dir, environment
		if output, err := command.CombinedOutput(); err != nil {
			t.Errorf("%s: %v\n%s", name, err, output)
		}
	}
}
