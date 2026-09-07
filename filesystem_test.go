package effect_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	effect "github.com/mbauer83/effect-golang"
	"github.com/mbauer83/effect-golang/effecttest"
)

// Config is an application environment, used here to show that the filesystem
// capability composes with one rather than requiring Unit.
type Config struct {
	Root string
}

func TestFilesystemCapabilitySurfaceOverARealDirectory(t *testing.T) {
	io := effect.IOFor[Config]()
	root := t.TempDir()

	nested := filepath.Join(root, "in", "deeper")
	program := io.MkdirAll(nested, 0o750).
		AndThen(io.WriteFile(filepath.Join(nested, "record.txt"), []byte("payload"), 0o600)).
		AndThen(io.Rename(
			filepath.Join(nested, "record.txt"),
			filepath.Join(nested, "renamed.txt"),
		)).
		AndThen(io.ReadDir(nested)).
		FlatMap(func(entries []fs.DirEntry) effect.Effect[Config, effect.IOError, fs.FileInfo] {
			if len(entries) != 1 || entries[0].Name() != "renamed.txt" {
				t.Errorf("unexpected directory contents: %#v", entries)
			}
			return io.Stat(filepath.Join(nested, "renamed.txt"))
		})

	exit := effect.Run(context.Background(), Config{Root: root}, program)
	info, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected failure: %v", exit)
	}
	if info.Size() != int64(len("payload")) {
		t.Fatalf("unexpected file size: %d", info.Size())
	}

	removal := effect.Run(context.Background(), Config{Root: root},
		io.Remove(filepath.Join(nested, "renamed.txt")))
	if removal.IsFailure() {
		t.Fatalf("unexpected removal failure: %v", removal)
	}
	if _, err := os.Stat(filepath.Join(nested, "renamed.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected the file to be gone, got %v", err)
	}
}

func TestIOErrorRecordsBothPathsAndKeepsThePlatformError(t *testing.T) {
	io := effect.IO()
	root := t.TempDir()
	oldPath := filepath.Join(root, "absent.txt")
	newPath := filepath.Join(root, "target.txt")

	exit := effect.Run(context.Background(), effect.Unit{}, io.Rename(oldPath, newPath))
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected a typed failure, got %v", exit)
	}
	failure, isLeaf := cause.Failure()
	if !isLeaf {
		t.Fatalf("expected one failure leaf, got %v", cause)
	}
	if failure.Operation != effect.IORename || failure.Path != oldPath || failure.TargetPath != newPath {
		t.Fatalf("expected both paths recorded unambiguously, got %#v", failure)
	}
	if !errors.Is(failure, fs.ErrNotExist) {
		t.Fatalf("expected Unwrap to keep the platform error, got %v", failure)
	}
	rendered := failure.Error()
	if !strings.Contains(rendered, oldPath) || !strings.Contains(rendered, newPath) {
		t.Fatalf("expected both paths in the message, got %q", rendered)
	}
}

func TestFileSystemStubInjectsBehaviourPerOperation(t *testing.T) {
	broken := errors.New("device is offline")
	stub := effecttest.FileSystemStub{
		ReadFileFunc: func(context.Context, string) ([]byte, error) {
			return nil, broken
		},
	}
	runtime, err := effect.NewRuntime(effect.WithFileSystem(stub))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())

	io := effect.IO()
	exit := runtime.Run(context.Background(), effect.Unit{}, io.ReadFile("anywhere"))
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected the injected failure, got %v", exit)
	}
	failure, _ := cause.Failure()
	if !errors.Is(failure, broken) {
		t.Fatalf("expected the injected error, got %v", failure)
	}

	// An operation the stub does not implement reports that explicitly rather
	// than pretending to succeed.
	unimplemented := runtime.Run(context.Background(), effect.Unit{}, io.Stat("anywhere"))
	statCause, _ := unimplemented.Cause()
	statFailure, _ := statCause.Failure()
	if !errors.Is(statFailure, effecttest.ErrUnimplementedOperation) {
		t.Fatalf("expected an unimplemented-operation failure, got %v", statFailure)
	}
}
