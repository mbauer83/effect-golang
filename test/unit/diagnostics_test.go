package unit

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effecttest"
)

func TestCauseReportPreservesTreeShapeForExporters(t *testing.T) {
	cause := effect.FailCause("read failed").Then(
		effect.DieCause[string](effect.Defect{Value: "close failed", Stack: "goroutine 1"}).Both(
			effect.InterruptCause[string](effect.ErrTimedOut),
		),
	)

	report := cause.Report()
	if report.Kind != effect.CauseThen || len(report.Children) != 2 {
		t.Fatalf("expected a Then with two children, got %#v", report)
	}
	if got := report.Children[0]; got.Kind != effect.CauseFailure || got.Detail != "read failed" {
		t.Fatalf("unexpected failure child: %#v", got)
	}

	parallel := report.Children[1]
	if parallel.Kind != effect.CauseBoth || len(parallel.Children) != 2 {
		t.Fatalf("expected a Both with two children, got %#v", parallel)
	}
	defect := parallel.Children[0]
	if defect.Kind != effect.CauseDefect || defect.Detail != "close failed" || defect.Stack != "goroutine 1" {
		t.Fatalf("unexpected defect child: %#v", defect)
	}
	interruption := parallel.Children[1]
	if interruption.Kind != effect.CauseInterrupted || !strings.Contains(interruption.Detail, "timed out") {
		t.Fatalf("unexpected interruption child: %#v", interruption)
	}
}

func TestCauseStatusRanksDefectAboveInterruption(t *testing.T) {
	statuses := map[effect.EventStatus]effect.Cause[string]{
		effect.EventStatusFailure:     effect.FailCause("rejected"),
		effect.EventStatusInterrupted: effect.InterruptCause[string](effect.ErrScopeClosed),
		effect.EventStatusDefect: effect.InterruptCause[string](effect.ErrScopeClosed).
			Both(effect.DieCause[string](effect.Defect{Value: "boom"})),
	}

	for want, cause := range statuses {
		if got := cause.Status(); got != want {
			t.Fatalf("expected %q for %v, got %q", want, cause, got)
		}
	}
}

func TestCauseRenderingIncludesPanicStacksOnlyWhenAsked(t *testing.T) {
	exit := effect.Run(context.Background(), effect.Unit{},
		effecttest.Panicking[effect.Unit, string, string]("body exploded"))

	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected a defect, got %v", exit)
	}
	concise := cause.String()
	if strings.Contains(concise, "goroutine") {
		t.Fatalf("expected no stack in concise rendering:\n%s", concise)
	}
	if plain := fmt.Sprintf("%v", cause); plain != concise {
		t.Fatalf("expected %%v to match String, got:\n%s", plain)
	}

	detailed := fmt.Sprintf("%+v", cause)
	if !strings.Contains(detailed, "goroutine") {
		t.Fatalf("expected %%+v to include the panic stack:\n%s", detailed)
	}
	if !strings.Contains(cause.Report().Stack, "goroutine") {
		t.Fatalf("expected the structured report to carry the stack too")
	}
}

func TestDebugTrackingReportsWorkAProgramLeftRunning(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}
	diagnostics := &effecttest.RecordingDiagnostics{}
	work := effecttest.NewBlocker(tracker)

	runtime, err := effect.NewRuntime(
		effect.WithDebugTracking(),
		effect.WithDiagnostics(diagnostics),
	)
	if err != nil {
		t.Fatal(err)
	}

	runtime.Run(context.Background(), effect.Unit{},
		operations.ForkDaemon(effecttest.Blocking[effect.Unit, string](work, "finished")).FlatMap(func(forkedFiber) forkedProgram {
			work.AwaitStart()
			return operations.Succeed("run finished")
		}),
	)

	if live := runtime.LiveWork(); live.Fibers != 1 {
		t.Fatalf("expected one detached fiber still owned, got %#v", live)
	}

	runtime.Close(context.Background())
	if live := runtime.LiveWork(); !live.IsEmpty() {
		t.Fatalf("expected Close to have drained every owned fiber, got %#v", live)
	}

	faults := diagnostics.Faults()
	if len(faults) != 1 || faults[0].Component != effect.FaultRuntime {
		t.Fatalf("expected one runtime fault describing the remainder, got %#v", faults)
	}
	if !strings.Contains(faults[0].Err.Error(), "1 fiber(s)") {
		t.Fatalf("expected the remainder to be described, got %v", faults[0].Err)
	}
}

func TestDebugTrackingSeesNoLeakForAWellScopedProgram(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}
	diagnostics := &effecttest.RecordingDiagnostics{}

	runtime, err := effect.NewRuntime(
		effect.WithDebugTracking(),
		effect.WithDiagnostics(diagnostics),
	)
	if err != nil {
		t.Fatal(err)
	}

	program := effect.Scoped(func(scope effect.Scope) forkedProgram {
		return effecttest.TrackedResource[effect.Unit, string](scope, tracker, "handle").FlatMap(func(string) forkedProgram {
			return operations.Fork(operations.Succeed("child")).FlatMap(operations.Join)
		})
	})

	if exit := runtime.Run(context.Background(), effect.Unit{}, program); exit.IsFailure() {
		t.Fatalf("unexpected failure: %v", exit)
	}
	if live := runtime.LiveWork(); !live.IsEmpty() {
		t.Fatalf("expected no live work after a scoped program, got %#v", live)
	}

	runtime.Close(context.Background())
	if faults := diagnostics.Faults(); len(faults) != 0 {
		t.Fatalf("expected no diagnostics for a well-scoped program, got %#v", faults)
	}
}

func TestUntrackedRuntimeReportsNothingLive(t *testing.T) {
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())

	if live := runtime.LiveWork(); !reflect.DeepEqual(live, effect.LiveWork{}) {
		t.Fatalf("expected an untracked runtime to count nothing, got %#v", live)
	}
}

func TestCauseRenderingIsTotalForAwkwardValues(t *testing.T) {
	type opaque struct{ Field []int }
	awkward := []effect.Cause[error]{
		{},
		effect.EmptyCause[error](),
		effect.FailCause[error](nil),
		effect.DieCause[error](effect.Defect{}),
		effect.DieCause[error](effect.Defect{Value: opaque{Field: nil}}),
		effect.InterruptCause[error](nil),
		effect.EmptyCause[error]().Then(effect.EmptyCause[error]()),
		effect.FailCause[error](nil).Both(effect.DieCause[error](effect.Defect{Value: 42})),
	}

	for index, cause := range awkward {
		if rendered := cause.String(); rendered == "" {
			t.Fatalf("case %d rendered nothing", index)
		}
		if formatted := fmt.Sprintf("%+v", cause); formatted == "" {
			t.Fatalf("case %d formatted to nothing", index)
		}
		if report := cause.Report(); report.Kind != cause.Kind() {
			t.Fatalf("case %d reported kind %v for %v", index, report.Kind, cause.Kind())
		}
	}
}

func TestSpanEventsCarryTheirCallSite(t *testing.T) {
	observer := &effecttest.RecordingObserver{}
	runtime, err := effect.NewRuntime(effect.WithObserver(observer))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())

	operations := effect.For[effect.Unit, string]()
	runtime.Run(context.Background(), effect.Unit{},
		operations.Succeed("done").WithSpan("import"))

	events := observer.Events()
	if len(events) == 0 {
		t.Fatal("expected span events")
	}
	for _, event := range events {
		if !strings.Contains(event.Source, "diagnostics_test.go") {
			t.Fatalf("expected the span's call site, got %q", event.Source)
		}
	}
}
