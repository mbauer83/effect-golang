// Command effectgo runs the go command with direct-style bodies rewritten into
// FlatMap chains.
//
//	effectgo test ./...
//	effectgo build -o server ./cmd/server
//	go test -overlay="$(effectgo overlay ./...)" ./...
//
// The rewrite is an optimisation. Every effect.Gen body is ordinary Go that
// compiles and runs correctly without it; effectgo produces a faster form of
// the bodies it can translate, hands the go command an -overlay naming the
// rewritten files, and leaves the source tree untouched. A body it declines
// keeps running on its own goroutine. EFFECTGO_EXPLAIN=1 lists every body
// declined, and why.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/mbauer83/effect-golang/tools/effectgo/rewrite"
)

// overlaid are the go commands that accept -overlay.
var overlaid = []string{"build", "install", "list", "run", "test", "vet"}

func main() {
	if err := run(os.Args[1:]); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "effectgo:", err)
		os.Exit(2)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: effectgo <go command> [arguments], or effectgo overlay [packages]")
	}
	if args[0] == "overlay" {
		patterns := args[1:]
		if len(patterns) == 0 {
			patterns = []string{"./..."}
		}
		if !understood() {
			patterns = nil
		}
		path, err := overlay(patterns)
		if err != nil {
			return err
		}
		fmt.Println(path)
		return nil
	}
	command := exec.Command("go", args...)
	if slices.Contains(overlaid, args[0]) && understood() {
		patterns, err := workspacePatterns()
		if err != nil {
			return err
		}
		path, err := overlay(patterns)
		if err != nil {
			return err
		}
		command = exec.Command("go", append([]string{args[0], "-overlay=" + path}, args[1:]...)...)
	}
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	return command.Run()
}

// workspacePatterns covers every module the go command is building from
// source, so a body in a dependency the build compiles is rewritten along with
// the packages named on the command line.
func workspacePatterns() ([]string, error) {
	output, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		return nil, fmt.Errorf("listing the modules being built: %w", err)
	}
	var patterns []string
	for _, dir := range strings.Fields(string(output)) {
		patterns = append(patterns, filepath.Join(dir, "..."))
	}
	return patterns, nil
}

// overlay rewrites the packages matching patterns and answers with the path of
// an overlay file naming every rewritten file.
func overlay(patterns []string) (string, error) {
	var importers []string
	if len(patterns) > 0 {
		matches, err := importersOfGen(patterns)
		if err != nil {
			return "", err
		}
		importers = matches
	}
	cache, err := cacheDir()
	if err != nil {
		return "", err
	}
	replace := map[string]string{}
	rewrites, declines := 0, 0
	if len(importers) > 0 {
		pkgs, err := packages.Load(&packages.Config{
			Mode:  packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
			Tests: true,
		}, importers...)
		if err != nil {
			return "", err
		}
		for _, pkg := range pkgs {
			for i, file := range pkg.Syntax {
				filename := pkg.CompiledGoFiles[i]
				if _, done := replace[filename]; done || !importsEffect(file) {
					continue
				}
				text, err := os.ReadFile(filename)
				if err != nil {
					return "", err
				}
				result := rewrite.File(pkg.Fset, file, pkg.TypesInfo, pkg.Types, text)
				rewrites += result.Rewrites
				declines += len(result.Declines)
				explain(result.Declines)
				if result.Source == nil {
					continue
				}
				target, err := store(cache, filename, result.Source)
				if err != nil {
					return "", err
				}
				replace[filename] = target
			}
		}
	}
	fmt.Fprintf(os.Stderr, "effectgo: rewrote %d Gen bodies, declined %d\n", rewrites, declines)
	overlayJSON, err := json.Marshal(map[string]map[string]string{"Replace": replace})
	if err != nil {
		return "", err
	}
	return store(cache, "overlay.json", overlayJSON)
}

// importersOfGen is the packages matching patterns that might call
// effect.Gen: those importing effect whose files mention Gen at all, found
// without type-checking anything. Every program imports effect, so the text
// is what keeps the type-checking to the packages that need it.
func importersOfGen(patterns []string) ([]string, error) {
	pkgs, err := packages.Load(&packages.Config{
		Mode:  packages.NeedName | packages.NeedImports | packages.NeedFiles,
		Tests: true,
	}, patterns...)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var importers []string
	for _, pkg := range pkgs {
		if _, ok := pkg.Imports[effectPath]; !ok || seen[pkg.PkgPath] || !mentionsGen(pkg.GoFiles) {
			continue
		}
		seen[pkg.PkgPath] = true
		importers = append(importers, pkg.PkgPath)
	}
	return importers, nil
}

func mentionsGen(files []string) bool {
	for _, file := range files {
		text, err := os.ReadFile(file)
		if err == nil && (bytes.Contains(text, []byte("Gen(")) || bytes.Contains(text, []byte("Gen["))) {
			return true
		}
	}
	return false
}

const effectPath = "github.com/mbauer83/effect-golang/effect"

func importsEffect(file *ast.File) bool {
	for _, spec := range file.Imports {
		if path, _ := strconv.Unquote(spec.Path.Value); path == effectPath {
			return true
		}
	}
	return false
}

func explain(declines []rewrite.Decline) {
	if os.Getenv("EFFECTGO_EXPLAIN") == "" {
		return
	}
	for _, decline := range declines {
		fmt.Fprintf(os.Stderr, "effectgo: %s: left as it was: %s\n", decline.Position, decline.Reason)
	}
}
