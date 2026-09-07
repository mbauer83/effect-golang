package unit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effecttest"
	"github.com/mbauer83/effect-golang/experimental/direct"
)

type directProgram = effect.Effect[effect.Unit, string, string]

var directOperations = effect.For[effect.Unit, string]()

func TestDirectStyleSequencesDependentSteps(t *testing.T) {
	program := direct.Run(func(bind *direct.Binder[effect.Unit, string]) string {
		first := direct.Bind(bind, directOperations.Succeed("alpha"))
		second := direct.Bind(bind, directOperations.Succeed(first+"-beta"))
		return second + "-gamma"
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if value, ok := exit.Value(); !ok || value != "alpha-beta-gamma" {
		t.Fatalf("unexpected exit: %v", exit)
	}
}

func TestDirectStyleIsLazyAndReusable(t *testing.T) {
	tracker := &effecttest.Tracker{}
	program := direct.Run(func(bind *direct.Binder[effect.Unit, string]) string {
		return direct.Bind(bind, directOperations.From(
			func(context.Context, effect.Unit) effect.Exit[string, string] {
				tracker.Record("evaluated")
				return effect.ExitSuccess[string]("done")
			},
		))
	})
	if got := tracker.Count("evaluated"); got != 0 {
		t.Fatalf("expected construction to run nothing, got %d", got)
	}

	for range 3 {
		if exit := effect.Run(context.Background(), effect.Unit{}, program); exit.IsFailure() {
			t.Fatalf("unexpected failure: %v", exit)
		}
	}
	if got := tracker.Count("evaluated"); got != 3 {
		t.Fatalf("expected one evaluation per run, got %d", got)
	}
}

func TestDirectStyleShortCircuitsOnATypedFailure(t *testing.T) {
	tracker := &effecttest.Tracker{}
	program := direct.Run(func(bind *direct.Binder[effect.Unit, string]) string {
		direct.Bind(bind, directOperations.Fail[string]("rejected"))
		tracker.Record("unreachable")
		return "unreachable"
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected the failure to short-circuit, got %v", exit)
	}
	if failure, ok := cause.Failure(); !ok || failure != "rejected" {
		t.Fatalf("unexpected cause: %v", cause)
	}
	if got := tracker.Count("unreachable"); got != 0 {
		t.Fatalf("expected the rest of the body to be abandoned, ran %d times", got)
	}
}

func TestDirectStylePropagatesDefectsAndInterruptionUnchanged(t *testing.T) {
	defecting := direct.Run(func(bind *direct.Binder[effect.Unit, string]) string {
		return direct.Bind(bind, effecttest.Panicking[effect.Unit, string, string]("source exploded"))
	})
	exit := effect.Run(context.Background(), effect.Unit{}, defecting)
	if cause, failed := exit.Cause(); !failed || !cause.ContainsDefect() {
		t.Fatalf("expected a defect, got %v", exit)
	}

	stop := errors.New("caller stopped")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(stop)
	interrupted := direct.Run(func(bind *direct.Binder[effect.Unit, string]) string {
		return direct.Bind(bind, directOperations.Succeed("unreachable"))
	})
	cause, failed := effect.Run(ctx, effect.Unit{}, interrupted).Cause()
	if !failed {
		t.Fatal("expected interruption")
	}
	if interruption, ok := cause.Interruption(); !ok || !errors.Is(interruption.Cause, stop) {
		t.Fatalf("expected the caller's cancellation cause, got %v", cause)
	}
}

func TestDirectStyleTurnsABodyPanicIntoADefect(t *testing.T) {
	program := direct.Run(func(*direct.Binder[effect.Unit, string]) string {
		panic("body exploded")
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed || !cause.ContainsDefect() {
		t.Fatalf("expected a defect, got %v", exit)
	}
	if defects := cause.Defects(); len(defects) != 1 || defects[0].Value != "body exploded" {
		t.Fatalf("unexpected defects: %#v", cause.Defects())
	}
}

func TestDirectStyleUsesTheSurroundingRuntimeAndScope(t *testing.T) {
	// The whole reason direct style needs a seam into the current
	// interpretation: a bound effect must see the runtime's capabilities and the
	// enclosing scope, not a fresh runtime with live defaults.
	tracker := &effecttest.Tracker{}
	logger := &effecttest.RecordingLogger{}
	runtime, clock := effecttest.NewTimedRuntime(t, effect.WithLogger(logger))

	program := effect.Scoped(func(scope effect.Scope) directProgram {
		return direct.Run(func(bind *direct.Binder[effect.Unit, string]) string {
			held := direct.Bind(bind, effecttest.TrackedResource[effect.Unit, string](scope, tracker, "handle"))
			direct.Bind(bind, directOperations.LogInfo("bound "+held))
			return direct.Bind(bind, directOperations.Now().Map(
				func(moment time.Time) string { return moment.Format(time.RFC3339) },
			))
		})
	})

	exit := runtime.Run(context.Background(), effect.Unit{}, program)
	value, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected failure: %v", exit)
	}
	if !strings.Contains(value, clock.Now().Format("2006")) {
		t.Fatalf("expected the runtime's clock, got %q", value)
	}
	if records := logger.Records(); len(records) != 1 || records[0].Message != "bound handle" {
		t.Fatalf("expected the runtime's logger, got %#v", records)
	}
	if released := tracker.Count("release handle"); released != 1 {
		t.Fatalf("expected the enclosing scope to release the resource, got %d", released)
	}
}

func TestDirectStyleReportsABinderUsedAfterItsBodyReturned(t *testing.T) {
	var escaped *direct.Binder[effect.Unit, string]

	leaking := direct.Run(func(bind *direct.Binder[effect.Unit, string]) string {
		escaped = bind
		return "captured"
	})
	if exit := effect.Run(context.Background(), effect.Unit{}, leaking); exit.IsFailure() {
		t.Fatalf("unexpected failure: %v", exit)
	}

	// Using the escaped binder must say so rather than evaluate against an
	// interpretation that has ended.
	later := effect.Interpreting(func(effect.Interpreter[effect.Unit, string]) effect.Exit[string, string] {
		return effect.ExitSuccess[string](direct.Bind(escaped, directOperations.Succeed("late")))
	})
	exit := effect.Run(context.Background(), effect.Unit{}, later)
	cause, failed := exit.Cause()
	if !failed || !cause.ContainsDefect() {
		t.Fatalf("expected a defect, got %v", exit)
	}
	if !strings.Contains(cause.String(), "outside the Run body") {
		t.Fatalf("expected the message to name the mistake, got %v", cause)
	}
}

func TestDirectStyleDetectsASwallowedShortCircuit(t *testing.T) {
	// A broad recover() in the body cannot be prevented. It can be detected,
	// which is the difference between reporting a defect and returning a value
	// the program never computed.
	program := direct.Run(func(bind *direct.Binder[effect.Unit, string]) (result string) {
		defer func() {
			if recover() != nil {
				result = "swallowed"
			}
		}()
		return direct.Bind(bind, directOperations.Fail[string]("rejected"))
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if value, ok := exit.Value(); ok {
		t.Fatalf("expected a defect rather than the value %q", value)
	}
	cause, _ := exit.Cause()
	if !cause.ContainsDefect() || !strings.Contains(cause.String(), "swallowed a Bind short-circuit") {
		t.Fatalf("expected the swallowed sentinel to be named, got %v", cause)
	}
}

func TestNestedDirectRunsDoNotCatchEachOthersShortCircuit(t *testing.T) {
	program := direct.Run(func(outer *direct.Binder[effect.Unit, string]) string {
		inner := direct.Run(func(bind *direct.Binder[effect.Unit, string]) string {
			direct.Bind(bind, directOperations.Fail[string]("inner rejected"))
			return "unreachable"
		})
		// The inner Run's failure is an ordinary typed failure out here, so the
		// outer body decides what it means.
		recovered := direct.Bind(outer, inner.CatchAll(
			func(failure string) directProgram {
				return directOperations.Succeed("handled: " + failure)
			},
		))
		return recovered
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if value, ok := exit.Value(); !ok || value != "handled: inner rejected" {
		t.Fatalf("unexpected exit: %v", exit)
	}
}
