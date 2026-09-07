package effect_test

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	effect "github.com/mbauer83/effect-golang"
	"github.com/mbauer83/effect-golang/effecttest"
)

func TestRetryEventsInheritSpanNameAndAnnotations(t *testing.T) {
	clock := effecttest.NewManualClock(time.Unix(0, 0))
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

	var attempts atomic.Int32
	load := effect.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		if attempts.Add(1) == 1 {
			return effect.ExitFailure[string, int]("transient")
		}
		return effect.ExitSuccess[string](23)
	})
	operations := effect.For[effect.Unit, string]()
	program := load.RetryN(1).
		Tap(func(value int) effect.Effect[effect.Unit, string, effect.Unit] {
			return operations.LogInfo("loaded", slog.Int("value", value))
		}).
		Named("load-record").
		Annotate(slog.String("component", "catalog")).
		WithSpan("catalog-load")

	exit := runtime.Run(context.Background(), effect.Unit{}, program)
	if !exit.IsSuccess() {
		t.Fatalf("unexpected observed exit: %+v", exit)
	}

	events := observer.Events()
	wantKinds := []effect.EventKind{
		effect.EventSpanStarted,
		effect.EventRetryScheduled,
		effect.EventRetrySucceeded,
		effect.EventLogEmitted,
		effect.EventSpanEnded,
	}
	if len(events) != len(wantKinds) {
		t.Fatalf("unexpected events: %#v", events)
	}
	for index, want := range wantKinds {
		if events[index].Kind != want || events[index].SpanID == 0 {
			t.Fatalf("unexpected event %d: %#v", index, events[index])
		}
	}
	if events[1].Attempt != 1 || events[2].Attempt != 2 {
		t.Fatalf("unexpected retry attempts: %#v", events)
	}
	if events[1].Operation != "load-record" || len(events[1].Attributes) != 1 {
		t.Fatalf("retry event lost metadata: %#v", events[1])
	}
}

type panickingObserver struct{}

func (panickingObserver) Observe(context.Context, effect.RuntimeEvent) {
	panic("observer unavailable")
}

func TestObserverPanicIsContainedAndReportedToDiagnostics(t *testing.T) {
	logger := &effecttest.RecordingLogger{}
	diagnostics := &effecttest.RecordingDiagnostics{}
	runtime, err := effect.NewRuntime(
		effect.WithLogger(logger),
		effect.WithObserver(panickingObserver{}),
		effect.WithDiagnostics(diagnostics),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())

	program := effect.LogInfo[effect.Unit, string]("still succeeds").WithSpan("observed")
	exit := runtime.Run(context.Background(), effect.Unit{}, program)
	if !exit.IsSuccess() || len(logger.Records()) != 1 {
		t.Fatalf("observer changed program semantics: exit=%+v records=%#v", exit, logger.Records())
	}

	faults := diagnostics.Faults()
	if len(faults) == 0 {
		t.Fatal("expected the contained observer panic to reach diagnostics")
	}
	if faults[0].Component != effect.FaultObserver {
		t.Fatalf("unexpected fault: %#v", faults[0])
	}
}
