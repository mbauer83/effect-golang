package effect_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Go source files in this module have a 250-line soft limit and a 350-line
// hard limit. Both currently hold with no exemptions, so both are enforced
// here: raising either bound is a deliberate edit to this test rather than
// something a large file can do by drifting past a review.
const (
	softLineLimit = 250
	hardLineLimit = 350
)

func TestSourceFilesStayWithinTheirLineLimits(t *testing.T) {
	oversized := map[string]int{}
	walk := func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		lines, err := countLines(path)
		if err != nil {
			return err
		}
		if lines > softLineLimit {
			oversized[path] = lines
		}
		return nil
	}
	if err := filepath.WalkDir(".", walk); err != nil {
		t.Fatal(err)
	}

	for path, lines := range oversized {
		limit := "soft"
		if lines > hardLineLimit {
			limit = "hard"
		}
		t.Errorf("%s has %d lines, past the %s limit; split it by domain role", path, lines, limit)
	}
}

func countLines(path string) (int, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strings.Count(string(content), "\n") + 1, nil
}
