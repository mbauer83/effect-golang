package acceptance

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effecttest"
	"github.com/mbauer83/effect-golang/examples/fanout"
)

func seedRecords(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "records.log")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

var records = []string{
	"INFO started",
	"WARN slow response",
	"INFO handled",
	"WARN retrying",
	"INFO finished",
	"WARN giving up",
}

func TestFanoutDeliversEveryRecordToBothReporters(t *testing.T) {
	inputPath := seedRecords(t, records...)
	observer := &effecttest.EventRecorder{}
	runtime, err := effect.NewRuntime(
		effect.WithObserver(observer),
		effect.WithDebugTracking(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())

	exit := runtime.Run(context.Background(), effect.Unit{}, fanout.Program(inputPath, 3))
	report, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected failure: %v", exit)
	}

	if report.Records != len(records) {
		t.Fatalf("expected every record counted once, got %d of %d", report.Records, len(records))
	}
	if report.Warnings != 3 {
		t.Fatalf("expected three warnings, got %d", report.Warnings)
	}
	// Both reporters read the same deferred label. combine reports a mismatch
	// in the label itself, so an inconsistency cannot pass silently.
	if !strings.Contains(report.Label, "records.log") || strings.Contains(report.Label, "inconsistent") {
		t.Fatalf("expected both reporters to read one consistent label, got %q", report.Label)
	}
	if !strings.Contains(report.Label, "6 records") {
		t.Fatalf("expected the label to describe the source, got %q", report.Label)
	}
	if live := runtime.LiveWork(); !live.IsEmpty() {
		t.Fatalf("expected the pipeline to leave nothing running, got %#v", live)
	}
	assertObservedKinds(t, observer, map[effect.EventKind]bool{
		effect.EventResourceAcquired: true,
		effect.EventResourceReleased: true,
		effect.EventFiberCompleted:   true,
		effect.EventScopeClosed:      true,
	})
}

func TestFanoutIsDeterministicAcrossWorkerCounts(t *testing.T) {
	// Workers race for records, so the counts must not depend on how many there
	// are or on who won.
	inputPath := seedRecords(t, records...)

	for _, workers := range []int{1, 2, 5, 16} {
		exit := effect.Run(context.Background(), effect.Unit{}, fanout.Program(inputPath, workers))
		report, ok := exit.Value()
		if !ok {
			t.Fatalf("%d workers: unexpected failure: %v", workers, exit)
		}
		if report.Records != len(records) || report.Warnings != 3 {
			t.Fatalf("%d workers: got %d records and %d warnings", workers, report.Records, report.Warnings)
		}
	}
}

func TestFanoutHandsAReadFailureToTheWaitingReporters(t *testing.T) {
	// The reporters wait on a value only the reader can supply. If the read
	// fails they must be given the failure, not left blocked.
	missing := filepath.Join(t.TempDir(), "absent.log")

	finished := make(chan effect.Exit[effect.IOError, fanout.Report], 1)
	go func() {
		finished <- effect.Run(context.Background(), effect.Unit{}, fanout.Program(missing, 2))
	}()

	select {
	case exit := <-finished:
		cause, failed := exit.Cause()
		if !failed {
			t.Fatalf("expected the read failure to surface, got %v", exit)
		}
		failures := cause.Failures()
		if len(failures) == 0 || !errors.Is(failures[0], fs.ErrNotExist) {
			t.Fatalf("expected the platform error to survive, got %v", cause)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("expected the pipeline to finish; a reporter is blocked on a value nobody will supply")
	}
}

func TestFanoutOnAnEmptySourceReportsNothing(t *testing.T) {
	inputPath := seedRecords(t, "")

	exit := effect.Run(context.Background(), effect.Unit{}, fanout.Program(inputPath, 2))
	report, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected failure: %v", exit)
	}
	if report.Records != 0 || report.Warnings != 0 {
		t.Fatalf("expected an empty report, got %#v", report)
	}
}

func TestFanoutLeavesNoGoroutineBehindWhenCancelled(t *testing.T) {
	inputPath := seedRecords(t, records...)
	stop := errors.New("operator cancelled the pipeline")
	before := runtime.NumGoroutine()

	for range 10 {
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(stop)
		if exit := effect.Run(ctx, effect.Unit{}, fanout.Program(inputPath, 4)); exit.IsSuccess() {
			t.Fatalf("expected interruption, got %v", exit)
		}
	}

	settleGoroutines()
	if after := runtime.NumGoroutine(); after > before+2 {
		t.Fatalf("expected no leaked workers, went from %d to %d", before, after)
	}
}
