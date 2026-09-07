package effect_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	effect "github.com/mbauer83/effect-golang"
	"github.com/mbauer83/effect-golang/effecttest"
	"github.com/mbauer83/effect-golang/examples/filecopy"
)

func TestFileCopyProgramExercisesBaseCapabilities(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.txt")
	outputPath := filepath.Join(directory, "output.txt")
	if err := os.WriteFile(inputPath, []byte("useful effect"), 0o600); err != nil {
		t.Fatal(err)
	}

	start := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)
	clock := effecttest.NewManualClock(start)
	logger := &effecttest.RecordingLogger{}
	observer := &effecttest.RecordingObserver{}
	runtime, err := effect.NewRuntime(
		effect.WithClock(clock),
		effect.WithLogger(logger),
		effect.WithObserver(observer),
	)
	if err != nil {
		t.Fatal(err)
	}

	exit := runtime.Run(context.Background(), effect.Unit{}, filecopy.Program(inputPath, outputPath))
	if !exit.IsSuccess() {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	written, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != "USEFUL EFFECT" {
		t.Fatalf("unexpected output: %q", written)
	}

	records := logger.Records()
	if len(records) != 1 || records[0].Message != "normalizing file" || !records[0].Timestamp.Equal(start) {
		t.Fatalf("unexpected log records: %#v", records)
	}
	if records[0].Operation != "file-copy" || records[0].SpanID == 0 {
		t.Fatalf("log record lost observation metadata: %#v", records[0])
	}
	events := observer.Events()
	wantKinds := []effect.EventKind{
		effect.EventSpanStarted,
		effect.EventLogEmitted,
		effect.EventSpanEnded,
	}
	if len(events) != len(wantKinds) {
		t.Fatalf("unexpected runtime events: %#v", events)
	}
	for index, want := range wantKinds {
		if events[index].Kind != want || events[index].SpanID != records[0].SpanID {
			t.Fatalf("unexpected event %d: %#v", index, events[index])
		}
	}
	if events[2].Status != effect.EventStatusSuccess {
		t.Fatalf("unexpected span status: %#v", events[2])
	}
}

func TestFileCopyProgramPreservesTypedFilesystemFailure(t *testing.T) {
	directory := t.TempDir()
	missing := filepath.Join(directory, "missing.txt")
	output := filepath.Join(directory, "output.txt")

	exit := effect.Run(context.Background(), effect.Unit{}, filecopy.Program(missing, output))
	cause, ok := exit.Cause()
	if !ok {
		t.Fatal("expected typed filesystem failure")
	}
	failure, ok := cause.Failure()
	if !ok || failure.Operation != effect.IOReadFile || failure.Path != missing {
		t.Fatalf("unexpected cause: %+v", cause)
	}
	if !errors.Is(failure, fs.ErrNotExist) {
		t.Fatalf("expected wrapped fs.ErrNotExist, got %v", failure)
	}
}

func TestManualClockSleepIsInterruptible(t *testing.T) {
	clock := effecttest.NewManualClock(time.Unix(0, 0))
	runtime, err := effect.NewRuntime(effect.WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	stop := errors.New("stop waiting")
	result := make(chan effect.Exit[string, effect.Unit], 1)
	go func() {
		result <- runtime.Run(ctx, effect.Unit{}, effect.Sleep[effect.Unit, string](time.Hour))
	}()

	waitCtx, stopWaiting := context.WithTimeout(context.Background(), time.Second)
	defer stopWaiting()
	if err := clock.WaitForPending(waitCtx, 1); err != nil {
		t.Fatalf("sleep did not register with manual clock: %v", err)
	}
	cancel(stop)
	exit := <-result
	cause, ok := exit.Cause()
	if !ok {
		t.Fatal("expected interruption")
	}
	interruption, ok := cause.Interruption()
	if !ok || !errors.Is(interruption.Cause, stop) || clock.PendingSleeps() != 0 {
		t.Fatalf("unexpected interrupted sleep: cause=%+v pending=%d", cause, clock.PendingSleeps())
	}
}
