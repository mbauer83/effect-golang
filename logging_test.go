package effect_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	effect "github.com/mbauer83/effect-golang"
	"github.com/mbauer83/effect-golang/effecttest"
)

func TestEveryLogLevelReachesTheRuntimeLoggerWithInheritedMetadata(t *testing.T) {
	logger := &effecttest.RecordingLogger{}
	clock := effecttest.NewManualClock(fixedMoment())
	runtime, err := effect.NewRuntime(effect.WithLogger(logger), effect.WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())

	operations := effect.For[effect.Unit, string]()
	program := operations.LogDebug("looking up").
		AndThen(operations.LogInfo("found", slog.Int("count", 2))).
		AndThen(operations.LogWarn("stale")).
		AndThen(operations.LogError("giving up")).
		Annotate(slog.String("component", "catalogue")).
		Named("load-catalogue").
		WithSpan("catalogue")

	if exit := runtime.Run(context.Background(), effect.Unit{}, program); exit.IsFailure() {
		t.Fatalf("unexpected failure: %v", exit)
	}

	records := logger.Records()
	wantLevels := []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
	if len(records) != len(wantLevels) {
		t.Fatalf("expected one record per level, got %d", len(records))
	}
	for index, want := range wantLevels {
		record := records[index]
		if record.Level != want {
			t.Fatalf("record %d has level %v, expected %v", index, record.Level, want)
		}
		if record.Operation != "load-catalogue" {
			t.Fatalf("record %d lost its operation name: %#v", index, record)
		}
		if record.SpanID == 0 {
			t.Fatalf("record %d lost its span identity: %#v", index, record)
		}
		if !record.Timestamp.Equal(fixedMoment()) {
			t.Fatalf("record %d used a clock other than the runtime's: %v", index, record.Timestamp)
		}
		// Ordered fields: the inherited annotation precedes the call's own.
		if len(record.Fields) == 0 || record.Fields[0].Key != "component" {
			t.Fatalf("record %d lost its inherited annotation: %#v", index, record.Fields)
		}
	}
	if fields := records[1].Fields; len(fields) != 2 || fields[1].Key != "count" {
		t.Fatalf("expected the call's own field last, got %#v", fields)
	}
}

// fixedMoment is the manual clock's start, so every record's timestamp is
// checkable rather than merely present.
func fixedMoment() time.Time {
	return time.Unix(1_700_000_000, 0).UTC()
}
