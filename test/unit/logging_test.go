package unit

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effect/capability"
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

func TestARecordSaysAKeyOnce(t *testing.T) {
	// A span's attributes are inherited by every record inside it, so a
	// record that names one of them again used to say it twice -- and
	// "method=GET route=/films method=GET route=/films" is noise a reader has
	// to check is not two different things.
	//
	// The record's own wins, because it was written about this line while the
	// inherited one was written about everything inside the span.
	sink := &collectedRecords{}
	runtime, err := effect.NewRuntime(effect.WithLogger(sink))
	if err != nil {
		t.Fatal(err)
	}

	program := effect.LogWarn[effect.Unit, error]("refused",
		slog.String("route", "/films/{film}"),
		slog.String("why", "no such film"),
	).Annotate(
		slog.String("route", "/films"),
		slog.String("method", "GET"),
	)

	if _, ok := runtime.Run(context.Background(), effect.Unit{}, program).Value(); !ok {
		t.Fatal("expected the log to be written")
	}

	records := sink.all()
	if len(records) != 1 {
		t.Fatalf("expected one record, got %d", len(records))
	}
	seen := map[string]int{}
	for _, field := range records[0].Fields {
		seen[field.Key]++
	}
	for key, count := range seen {
		if count != 1 {
			t.Errorf("expected %q once, got %d times", key, count)
		}
	}
	// And the nearer one is the one kept.
	for _, field := range records[0].Fields {
		if field.Key == "route" && field.Value.String() != "/films/{film}" {
			t.Errorf("expected the record's own route, got %q", field.Value.String())
		}
	}
	// The inherited one that was not restated is still there.
	if _, held := seen["method"]; !held {
		t.Error("expected the inherited method kept")
	}
}

// collectedRecords is where a program's records go, kept so a test can read
// them.
type collectedRecords struct {
	mutex   sync.Mutex
	records []capability.LogRecord
}

func (sink *collectedRecords) Log(_ context.Context, record capability.LogRecord) error {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	sink.records = append(sink.records, record)
	return nil
}

func (sink *collectedRecords) all() []capability.LogRecord {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	return append([]capability.LogRecord(nil), sink.records...)
}
