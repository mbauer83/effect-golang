package effect_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	effect "github.com/mbauer83/effect-golang"
	"github.com/mbauer83/effect-golang/examples/pipeline"
)

func TestPipelineStreamsRecordsThroughANativeChannel(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "records.txt")
	content := "first record\nsecond record here\n\nthird\n"
	if err := os.WriteFile(inputPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	exit := effect.Run(context.Background(), effect.Unit{}, pipeline.Program(inputPath, 2))
	summary, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected failure: %v", exit)
	}
	if summary.Records != 3 || summary.Words != 6 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
}

func TestPipelineSurfacesAProducerFailureRatherThanAnEmptyStream(t *testing.T) {
	// The consumer sees a closed channel either way. Joining the producer is
	// what keeps a read failure from looking like a source with no records.
	missing := filepath.Join(t.TempDir(), "absent.txt")

	exit := effect.Run(context.Background(), effect.Unit{}, pipeline.Program(missing, 1))
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected the producer's failure to surface, got %v", exit)
	}
	failures := cause.Failures()
	if len(failures) != 1 || !errors.Is(failures[0], fs.ErrNotExist) {
		t.Fatalf("expected the read failure, got %#v", failures)
	}
}

func TestPipelineHandlesAnUnbufferedChannel(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "records.txt")
	if err := os.WriteFile(inputPath, []byte("only one\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	exit := effect.Run(context.Background(), effect.Unit{}, pipeline.Program(inputPath, 0))
	if summary, ok := exit.Value(); !ok || summary.Records != 1 {
		t.Fatalf("unexpected exit: %v", exit)
	}
}

func TestPipelineCancellationLeavesNoGoroutineBehind(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "records.txt")
	if err := os.WriteFile(inputPath, []byte("a\nb\nc\nd\ne\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stop := errors.New("consumer went away")
	before := runtime.NumGoroutine()
	for range 20 {
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(stop)
		exit := effect.Run(ctx, effect.Unit{}, pipeline.Program(inputPath, 0))
		if exit.IsSuccess() {
			t.Fatalf("expected interruption, got %v", exit)
		}
	}

	settleGoroutines()
	if after := runtime.NumGoroutine(); after > before+2 {
		t.Fatalf("expected no leaked producers, went from %d to %d", before, after)
	}
}
