package unit

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effecttest"
)

func TestScopeReleasesResourcesInReverseAcquisitionOrder(t *testing.T) {
	tracker := &effecttest.Tracker{}
	program := effect.Scoped(func(scope effect.Scope) scopedProgram {
		return effecttest.TrackedResource[effect.Unit, string](scope, tracker, "database").
			FlatMap(func(string) scopedProgram {
				return effecttest.TrackedResource[effect.Unit, string](scope, tracker, "transaction")
			}).
			FlatMap(func(string) scopedProgram {
				return effecttest.TrackedResource[effect.Unit, string](scope, tracker, "statement")
			})
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if value, ok := exit.Value(); !ok || value != "statement" {
		t.Fatalf("unexpected exit: %v", exit)
	}

	want := []string{
		"acquire database", "acquire transaction", "acquire statement",
		"release statement", "release transaction", "release database",
	}
	if got := tracker.Events(); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected LIFO release,\nwant %v\ngot  %v", want, got)
	}
}

func TestScopeReleasesExactlyOnceForEveryOutcome(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	outcomes := map[string]scopedProgram{
		"success":     operations.Succeed("done"),
		"failure":     operations.Fail[string]("rejected"),
		"defect":      effecttest.Panicking[effect.Unit, string, string]("body exploded"),
		"interrupted": effecttest.SelfInterrupting[effect.Unit, string, string](),
	}

	for name, body := range outcomes {
		tracker := &effecttest.Tracker{}
		program := effect.Scoped(func(scope effect.Scope) scopedProgram {
			return effecttest.TrackedResource[effect.Unit, string](scope, tracker, "handle").AndThen(body)
		})

		effect.Run(context.Background(), effect.Unit{}, program)
		if got := tracker.Count("release handle"); got != 1 {
			t.Fatalf("%s: expected exactly one release, got %d", name, got)
		}
	}
}

func TestScopePreservesFinalizerDefectAfterOriginalFailure(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	brokenClose := errors.New("close failed")
	program := effect.Scoped(func(scope effect.Scope) scopedProgram {
		resource := scope.AcquireRelease(
			operations.Succeed("handle"),
			func(string) effect.Effect[effect.Unit, effect.Never, effect.Unit] {
				return effect.Release[effect.Unit](func(context.Context) error {
					return brokenClose
				})
			},
		)
		return resource.AndThen(operations.Fail[string]("rejected"))
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed || cause.Kind() != effect.CauseThen {
		t.Fatalf("expected Then(original, cleanup), got %v", exit)
	}
	if failures := cause.Failures(); !reflect.DeepEqual(failures, []string{"rejected"}) {
		t.Fatalf("expected the original typed failure to survive, got %#v", failures)
	}
	defects := cause.Defects()
	if len(defects) != 1 {
		t.Fatalf("expected one release defect, got %#v", defects)
	}
	if err, ok := defects[0].Value.(error); !ok || !errors.Is(err, brokenClose) {
		t.Fatalf("expected the wrapped close error, got %#v", defects[0].Value)
	}
}

func TestSuccessfulBodyFailsWhenReleaseDefects(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	program := effect.Scoped(func(scope effect.Scope) scopedProgram {
		return scope.AcquireRelease(
			operations.Succeed("handle"),
			func(string) effect.Effect[effect.Unit, effect.Never, effect.Unit] {
				return effect.Release[effect.Unit](func(context.Context) error {
					return errors.New("close failed")
				})
			},
		)
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed || !cause.ContainsDefect() {
		t.Fatalf("expected a release defect to surface, got %v", exit)
	}
}

func TestNestedScopesReleaseInnerLifetimeFirst(t *testing.T) {
	tracker := &effecttest.Tracker{}
	program := effect.Scoped(func(outer effect.Scope) scopedProgram {
		return effecttest.TrackedResource[effect.Unit, string](outer, tracker, "outer").FlatMap(func(string) scopedProgram {
			return effect.Scoped(func(inner effect.Scope) scopedProgram {
				return effecttest.TrackedResource[effect.Unit, string](inner, tracker, "inner")
			})
		})
	})

	effect.Run(context.Background(), effect.Unit{}, program)
	want := []string{"acquire outer", "acquire inner", "release inner", "release outer"}
	if got := tracker.Events(); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected nested release order,\nwant %v\ngot  %v", want, got)
	}
}

func TestAcquisitionAfterClosureReleasesImmediatelyAndReportsClosedScope(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}

	var escaped effect.Scope
	effect.Run(context.Background(), effect.Unit{}, effect.Scoped(
		func(scope effect.Scope) scopedProgram {
			escaped = scope
			return operations.Succeed("opened")
		},
	))

	exit := effect.Run(context.Background(), effect.Unit{}, effecttest.TrackedResource[effect.Unit, string](escaped, tracker, "late"))
	if got := tracker.Events(); !reflect.DeepEqual(got, []string{"acquire late", "release late"}) {
		t.Fatalf("expected immediate release of a late acquisition, got %v", got)
	}

	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected the closed lifetime to be reported, got %v", exit)
	}
	interruption, ok := cause.Interruption()
	if !ok || !errors.Is(interruption.Cause, effect.ErrScopeClosed) {
		t.Fatalf("expected ErrScopeClosed, got %v", cause)
	}
}

func TestScopeReleasesUnderCallerCancellation(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}
	stop := errors.New("operator stopped the import")
	ctx, cancel := context.WithCancelCause(context.Background())

	program := effect.Scoped(func(scope effect.Scope) scopedProgram {
		return effecttest.TrackedResource[effect.Unit, string](scope, tracker, "handle").FlatMap(func(string) scopedProgram {
			cancel(stop)
			return operations.Succeed("unreachable")
		})
	})

	exit := effect.Run(ctx, effect.Unit{}, program)
	if got := tracker.Count("release handle"); got != 1 {
		t.Fatalf("expected release despite cancellation, got %d", got)
	}
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected interruption, got %v", exit)
	}
	if interruption, ok := cause.Interruption(); !ok || !errors.Is(interruption.Cause, stop) {
		t.Fatalf("expected the caller's cancellation cause, got %v", cause)
	}
}
