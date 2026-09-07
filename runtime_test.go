package effect_test

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"time"

	effect "github.com/mbauer83/effect-golang"
	"github.com/mbauer83/effect-golang/effecttest"
)

func TestScopedLayerResourcesLiveAsLongAsTheirConsumer(t *testing.T) {
	tracker := &effecttest.Tracker{}
	operations := effect.For[string, string]()

	connection := effect.LayerScoped(func(scope effect.Scope) effect.Effect[effect.Unit, string, string] {
		return scope.AcquireRelease(
			effect.For[effect.Unit, string]().From(
				func(context.Context, effect.Unit) effect.Exit[string, string] {
					tracker.Record("open connection")
					return effect.ExitSuccess[string]("connection")
				},
			),
			func(string) effect.Effect[effect.Unit, effect.Never, effect.Unit] {
				return recordingRelease(tracker, "close connection")
			},
		)
	})

	consumer := operations.From(func(_ context.Context, env string) effect.Exit[string, string] {
		tracker.Record("query using " + env)
		return effect.ExitSuccess[string]("rows")
	})

	exit := effect.Run(context.Background(), effect.Unit{}, consumer.ProvideLayerSame(connection))
	if value, ok := exit.Value(); !ok || value != "rows" {
		t.Fatalf("unexpected exit: %v", exit)
	}

	want := []string{"open connection", "query using connection", "close connection"}
	if got := tracker.Events(); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the service to outlive construction,\nwant %v\ngot  %v", want, got)
	}
}

func TestDaemonFiberOutlivesRunAndIsBoundedByClose(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}
	work := newBlocker(tracker)

	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}

	var daemon forkedFiber
	exit := runtime.Run(context.Background(), effect.Unit{},
		operations.ForkDaemon(work.program()).FlatMap(
			func(fiber forkedFiber) forkedProgram {
				daemon = fiber
				work.awaitStart()
				return operations.Succeed("run finished")
			},
		),
	)
	if value, ok := exit.Value(); !ok || value != "run finished" {
		t.Fatalf("unexpected exit: %v", exit)
	}
	if _, completed := daemon.Poll(); completed {
		t.Fatal("expected the daemon to outlive the Run that created it")
	}

	if cleanup := runtime.Close(context.Background()); !cleanup.IsEmpty() {
		t.Fatalf("unexpected cleanup cause: %v", cleanup)
	}
	if _, completed := daemon.Poll(); !completed {
		t.Fatal("expected Close to have awaited the daemon")
	}
	if got := tracker.Count("interrupted"); got != 1 {
		t.Fatalf("expected Close to interrupt the daemon, got %d", got)
	}
}

func TestCapabilityOverridesAreIsolatedBetweenConcurrentRuntimes(t *testing.T) {
	const runtimes = 8
	operations := effect.For[effect.Unit, string]()
	var waiters sync.WaitGroup
	observed := make([]time.Time, runtimes)

	for index := range runtimes {
		start := time.Unix(int64(index)*1000, 0)
		waiters.Go(func() {
			clock := effecttest.NewManualClock(start)
			logger := &effecttest.RecordingLogger{}
			runtime, err := effect.NewRuntime(effect.WithClock(clock), effect.WithLogger(logger))
			if err != nil {
				t.Error(err)
				return
			}
			defer runtime.Close(context.Background())

			program := operations.LogInfo("working").AndThen(operations.Now())
			exit := runtime.Run(context.Background(), effect.Unit{}, program)
			observed[index], _ = exit.Value()

			if records := logger.Records(); len(records) != 1 || !records[0].Timestamp.Equal(start) {
				t.Errorf("runtime %d observed another runtime's logger: %#v", index, records)
			}
		})
	}
	waiters.Wait()

	for index, moment := range observed {
		if !moment.Equal(time.Unix(int64(index)*1000, 0)) {
			t.Fatalf("runtime %d observed another runtime's clock: %v", index, moment)
		}
	}
}

func TestNilCapabilityIsRejectedAtConstruction(t *testing.T) {
	for name, option := range map[string]effect.RuntimeOption{
		"clock":       effect.WithClock(nil),
		"filesystem":  effect.WithFileSystem(nil),
		"logger":      effect.WithLogger(nil),
		"observer":    effect.WithObserver(nil),
		"diagnostics": effect.WithDiagnostics(nil),
	} {
		if _, err := effect.NewRuntime(option); err == nil {
			t.Fatalf("expected a nil %s to be rejected at construction", name)
		}
	}
}

type bufferingLogger struct {
	mutex   sync.Mutex
	flushed int
	failure error
}

func (logger *bufferingLogger) Log(context.Context, effect.LogRecord) error {
	return nil
}

func (logger *bufferingLogger) Flush(context.Context) error {
	logger.mutex.Lock()
	defer logger.mutex.Unlock()
	logger.flushed++
	return logger.failure
}

func TestCloseFlushesBufferingCapabilities(t *testing.T) {
	logger := &bufferingLogger{}
	runtime, err := effect.NewRuntime(effect.WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}

	if cleanup := runtime.Close(context.Background()); !cleanup.IsEmpty() {
		t.Fatalf("unexpected cleanup cause: %v", cleanup)
	}
	if logger.flushed != 1 {
		t.Fatalf("expected exactly one flush, got %d", logger.flushed)
	}
}

func TestCloseReportsAFailedFlushAsADefect(t *testing.T) {
	broken := errors.New("queue drain failed")
	logger := &bufferingLogger{failure: broken}
	runtime, err := effect.NewRuntime(effect.WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}

	cleanup := runtime.Close(context.Background())
	if !cleanup.ContainsDefect() {
		t.Fatalf("expected a flush failure to surface as a defect, got %v", cleanup)
	}
}

type failingLogger struct {
	failure error
}

func (logger failingLogger) Log(context.Context, effect.LogRecord) error {
	return logger.failure
}

func TestLoggerFailureIsReportedToDiagnosticsAndNotToTheProgram(t *testing.T) {
	broken := errors.New("sink unavailable")
	diagnostics := &effecttest.RecordingDiagnostics{}
	runtime, err := effect.NewRuntime(
		effect.WithLogger(failingLogger{failure: broken}),
		effect.WithDiagnostics(diagnostics),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())

	operations := effect.For[effect.Unit, string]()
	exit := runtime.Run(context.Background(), effect.Unit{},
		operations.LogInfo("working", slog.String("stage", "import")).As("done"))

	if value, ok := exit.Value(); !ok || value != "done" {
		t.Fatalf("expected a logging fault to leave the program's exit alone, got %v", exit)
	}
	faults := diagnostics.Faults()
	if len(faults) != 1 {
		t.Fatalf("expected one reported fault, got %#v", faults)
	}
	fault := faults[0]
	if fault.Component != effect.FaultLogger || !errors.Is(fault.Err, broken) {
		t.Fatalf("unexpected fault: %#v", fault)
	}
}
