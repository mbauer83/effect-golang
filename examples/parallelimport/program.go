// Package parallelimport is a complete program showing structured concurrency
// over a scoped resource.
//
// A lock file marks the import as in progress. It is acquired before any source
// is read and released exactly once afterwards, after every child fiber has
// finished, whatever the outcome. The sources themselves are read concurrently
// with a bounded degree of parallelism, and their results are reassembled in
// input order.
package parallelimport

import (
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mbauer83/effect-golang/effect"
)

// Report is the aggregate outcome of one import.
type Report struct {
	Sources []string
	Bytes   int
}

// Source is one imported file's contribution to the report.
type Source struct {
	Name  string
	Bytes int
}

// Program imports every source concurrently under one lock.
//
// The lock is a scoped resource, so it cannot outlive the import and cannot be
// released while a reader is still running: closing a scope cancels and awaits
// its children before it releases anything they might be using.
func Program(sources []string, lockPath string, readers int) effect.Effect[effect.Unit, effect.IOError, Report] {
	io := effect.IO()
	return effect.Scoped(func(scope effect.Scope) effect.Effect[effect.Unit, effect.IOError, Report] {
		return acquireLock(io, scope, lockPath).
			AndThen(readAll(io, sources, readers)).
			Map(summarize).
			WithName("parallel-import").
			WithSpan("parallel-import", slog.Int("sources", len(sources)))
	})
}

func acquireLock(
	io effect.IOOperations[effect.Unit],
	scope effect.Scope,
	lockPath string,
) effect.Effect[effect.Unit, effect.IOError, string] {
	return scope.AcquireRelease(
		io.WriteFile(lockPath, []byte("import in progress\n"), 0o600).As(lockPath),
		func(path string) effect.Effect[effect.Unit, effect.Never, effect.Unit] {
			// Removing the lock cannot widen the import's error channel, so a
			// failure to remove it becomes a defect rather than being dropped.
			return io.FailuresAsDefects(io.Remove(path))
		},
	).WithName("acquire-import-lock")
}

func readAll(
	io effect.IOOperations[effect.Unit],
	sources []string,
	readers int,
) effect.Effect[effect.Unit, effect.IOError, []Source] {
	return effect.ForEachParN(sources, readers, func(path string) effect.Effect[effect.Unit, effect.IOError, Source] {
		return io.ReadFile(path).
			Map(func(content []byte) Source {
				return Source{Name: filepath.Base(path), Bytes: len(content)}
			}).
			WithName("read-source")
	})
}

// summarize reassembles the concurrent results deterministically. ForEachParN
// already preserves input order; sorting by name additionally makes the report
// independent of the caller's argument order.
func summarize(sources []Source) Report {
	report := Report{Sources: make([]string, 0, len(sources))}
	for _, source := range sources {
		report.Sources = append(report.Sources, source.Name)
		report.Bytes += source.Bytes
	}
	sort.Strings(report.Sources)
	return report
}

// Describe renders a report as one human-readable line.
func Describe(report Report) string {
	return strings.Join(report.Sources, ", ")
}
