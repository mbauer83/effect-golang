// Command effectgo runs the go command with direct-style bodies rewritten into
// FlatMap chains.
//
//	effectgo test ./...
//	effectgo build -o server ./cmd/server
//	go test -overlay="$(effectgo overlay ./...)" ./...
//
// The rewrite is an optimisation. Every direct.Run body is ordinary Go that
// compiles and runs correctly without it; effectgo produces a faster form of
// the bodies it can translate, hands the go command an -overlay naming the
// rewritten files, and leaves the source tree untouched. A body it declines
// keeps running on direct's goroutine. EFFECTGO_EXPLAIN=1 lists every body
// declined, and why.
package main

import (
	"crypto/sha256"
	"encoding/hex"
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
		path, err := overlay(patterns)
		if err != nil {
			return err
		}
		fmt.Println(path)
		return nil
	}
	command := exec.Command("go", args...)
	if slices.Contains(overlaid, args[0]) {
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
	importers, err := importersOfDirect(patterns)
	if err != nil {
		return "", err
	}
	cache, err := cacheDir()
	if err != nil {
		return "", err
	}
	replace := map[string]string{}
	rewritten, declined := 0, 0
	if len(importers) > 0 {
		loaded, err := packages.Load(&packages.Config{
			Mode:  packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
			Tests: true,
		}, importers...)
		if err != nil {
			return "", err
		}
		for _, pkg := range loaded {
			for i, file := range pkg.Syntax {
				filename := pkg.CompiledGoFiles[i]
				if _, done := replace[filename]; done || !importsDirect(file) {
					continue
				}
				text, err := os.ReadFile(filename)
				if err != nil {
					return "", err
				}
				result := rewrite.File(pkg.Fset, file, pkg.TypesInfo, pkg.Types, text)
				rewritten += result.Rewritten
				declined += len(result.Declined)
				explain(result.Declined)
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
	fmt.Fprintf(os.Stderr, "effectgo: rewrote %d direct bodies, declined %d\n", rewritten, declined)
	encoded, err := json.Marshal(map[string]map[string]string{"Replace": replace})
	if err != nil {
		return "", err
	}
	return store(cache, "overlay.json", encoded)
}

// importersOfDirect is the packages matching patterns that import direct, found
// without type-checking anything.
func importersOfDirect(patterns []string) ([]string, error) {
	loaded, err := packages.Load(&packages.Config{
		Mode:  packages.NeedName | packages.NeedImports,
		Tests: true,
	}, patterns...)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var importers []string
	for _, pkg := range loaded {
		if _, ok := pkg.Imports[directPath]; ok && !seen[pkg.PkgPath] {
			seen[pkg.PkgPath] = true
			importers = append(importers, pkg.PkgPath)
		}
	}
	return importers, nil
}

const directPath = "github.com/mbauer83/effect-golang/experimental/direct"

func importsDirect(file *ast.File) bool {
	for _, spec := range file.Imports {
		if path, _ := strconv.Unquote(spec.Path.Value); path == directPath {
			return true
		}
	}
	return false
}

func explain(declined []rewrite.Decline) {
	if os.Getenv("EFFECTGO_EXPLAIN") == "" {
		return
	}
	for _, decline := range declined {
		fmt.Fprintf(os.Stderr, "effectgo: %s: left as it was: %s\n", decline.Position, decline.Reason)
	}
}

func cacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "effectgo")
	return dir, os.MkdirAll(dir, 0o755)
}

// store writes content under a name derived from it, so a file rewritten the
// same way twice is written once and an overlay never names a stale file.
func store(dir, name string, content []byte) (string, error) {
	sum := sha256.Sum256(append([]byte(name+"\x00"), content...))
	target := filepath.Join(dir, hex.EncodeToString(sum[:12])+"-"+filepath.Base(name))
	if _, err := os.Stat(target); err == nil {
		return target, nil
	}
	temporary, err := os.CreateTemp(dir, "writing-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	return target, os.Rename(temporary.Name(), target)
}
