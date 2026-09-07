package effect_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	effect "github.com/mbauer83/effect-golang"
	"github.com/mbauer83/effect-golang/effecttest"
	"github.com/mbauer83/effect-golang/examples/parallelimport"
)

// seedSources writes deterministic fixtures into a temporary directory, so the
// scenario never depends on the network, the user's home directory or a shared
// file.
func seedSources(t *testing.T, contents map[string]string) (string, []string) {
	t.Helper()
	workspace := t.TempDir()
	names := make([]string, 0, len(contents))
	for name, content := range contents {
		path := filepath.Join(workspace, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		names = append(names, path)
	}
	return workspace, names
}

func TestParallelImportReleasesItsLockAfterEveryReader(t *testing.T) {
	workspace, sources := seedSources(t, map[string]string{
		"alpha.txt": "alpha\n",
		"beta.txt":  "beta beta\n",
	})
	lockPath := filepath.Join(workspace, "import.lock")

	observer := &effecttest.RecordingObserver{}
	runtime, err := effect.NewRuntime(
		effect.WithObserver(observer),
		effect.WithDebugTracking(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())

	exit := runtime.Run(context.Background(), effect.Unit{},
		parallelimport.Program(sources, lockPath, 2))

	report, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected failure: %v", exit)
	}
	want := []string{"alpha.txt", "beta.txt"}
	if !reflect.DeepEqual(report.Sources, want) {
		t.Fatalf("expected %v, got %v", want, report.Sources)
	}
	if report.Bytes != len("alpha\n")+len("beta beta\n") {
		t.Fatalf("unexpected byte count: %d", report.Bytes)
	}

	if _, err := os.Stat(lockPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected the lock to be released, got %v", err)
	}
	if live := runtime.LiveWork(); !live.IsEmpty() {
		t.Fatalf("expected no live work after the import, got %#v", live)
	}
	assertObservedKinds(t, observer, map[effect.EventKind]bool{
		effect.EventResourceAcquired: true,
		effect.EventResourceReleased: true,
		effect.EventScopeClosed:      true,
		effect.EventFiberCompleted:   true,
	})
}

func assertObservedKinds(t *testing.T, observer *effecttest.RecordingObserver, required map[effect.EventKind]bool) {
	t.Helper()
	seen := map[effect.EventKind]bool{}
	for _, event := range observer.Events() {
		seen[event.Kind] = true
	}
	for kind := range required {
		if !seen[kind] {
			t.Fatalf("expected a %s event among %v", kind, seen)
		}
	}
}

func TestParallelImportReleasesItsLockWhenASourceIsMissing(t *testing.T) {
	workspace, sources := seedSources(t, map[string]string{"alpha.txt": "alpha\n"})
	sources = append(sources, filepath.Join(workspace, "absent.txt"))
	lockPath := filepath.Join(workspace, "import.lock")

	exit := effect.Run(context.Background(), effect.Unit{},
		parallelimport.Program(sources, lockPath, 2))

	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected a typed filesystem failure, got %v", exit)
	}
	failures := cause.Failures()
	if len(failures) != 1 || failures[0].Operation != effect.IOReadFile {
		t.Fatalf("expected one read failure, got %#v", failures)
	}
	if !errors.Is(failures[0], fs.ErrNotExist) {
		t.Fatalf("expected the platform error to survive, got %v", failures[0])
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected the lock to be released after a failure, got %v", err)
	}
}

func TestParallelImportReleasesItsLockWhenTheCallerCancels(t *testing.T) {
	workspace, sources := seedSources(t, map[string]string{"alpha.txt": "alpha\n"})
	lockPath := filepath.Join(workspace, "import.lock")

	stop := errors.New("operator canceled the import")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(stop)

	exit := effect.Run(ctx, effect.Unit{}, parallelimport.Program(sources, lockPath, 2))
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected interruption, got %v", exit)
	}
	if interruption, ok := cause.Interruption(); !ok || !errors.Is(interruption.Cause, stop) {
		t.Fatalf("expected the caller's cancellation cause, got %v", cause)
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected no lock to be left behind, got %v", err)
	}
}
