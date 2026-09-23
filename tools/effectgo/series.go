package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/mod/semver"
)

// series is the effect-golang release series whose Gen this rewrite
// translates. A rewrite is only as sound as its model of what it rewrites, so
// against any other series effectgo rewrites nothing and says so: the code as
// written is always correct, only slower.
const series = "v0.4"

// understood reports whether the build uses an effect-golang this rewrite
// knows. A module being worked on -- the main module, or one a workspace or a
// replace directive points at a directory for -- has no version, and is taken
// to be the one this effectgo was built beside.
func understood() bool {
	output, err := exec.Command("go", "list", "-m", "-f", "{{.Version}}{{with .Replace}} {{.Dir}}{{end}}", effectModule).Output()
	if err != nil {
		return true
	}
	fields := strings.Fields(string(output))
	if len(fields) != 1 {
		return true
	}
	if version := fields[0]; semver.MajorMinor(version) != series {
		fmt.Fprintf(os.Stderr, "effectgo: this build uses effect-golang %s, and this effectgo rewrites %s.x; running unchanged\n", version, series)
		return false
	}
	return true
}

const effectModule = "github.com/mbauer83/effect-golang"
