package architecture

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// Go source files in this module have a 250-line soft limit and a 350-line hard
// limit. Both currently hold with no exemptions, so both are enforced here:
// raising either bound is a deliberate edit to this test rather than something
// a growing file can do by drifting past a review.
const (
	softLineLimit = 250
	hardLineLimit = 350
)

func TestSourceFilesStayWithinTheirLineLimits(t *testing.T) {
	root := moduleRoot(t)
	walk := func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		lines := strings.Count(readSource(t, path), "\n") + 1
		if lines <= softLineLimit {
			return nil
		}
		limit := "soft"
		if lines > hardLineLimit {
			limit = "hard"
		}
		t.Errorf("%s has %d lines, past the %s limit; split it by domain role",
			display(t, path), lines, limit)
		return nil
	}
	if err := filepath.WalkDir(root, walk); err != nil {
		t.Fatal(err)
	}
}

// Documentation the index does not reach is documentation nobody reads.
func TestEveryPublicDocumentIsLinkedFromTheReadme(t *testing.T) {
	root := moduleRoot(t)
	readme := readSource(t, filepath.Join(root, "README.md"))

	walk := func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if !strings.Contains(readme, filepath.ToSlash(relative)) {
			t.Errorf("%s is not linked from README.md", relative)
		}
		return nil
	}
	if err := filepath.WalkDir(filepath.Join(root, "docs"), walk); err != nil {
		t.Fatal(err)
	}
}
