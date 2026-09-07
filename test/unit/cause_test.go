package unit

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	effect "github.com/mbauer83/effect-golang"
)

func TestCauseZeroValueIsEmptyCompositionIdentity(t *testing.T) {
	var empty effect.Cause[string]
	failure := effect.FailCause("invalid")

	if !empty.IsEmpty() || empty.Kind() != effect.CauseEmpty {
		t.Fatalf("expected zero Cause to be Empty, got %v", empty)
	}
	if got := empty.Then(failure); got.String() != failure.String() {
		t.Fatalf("expected Empty Then failure to equal failure, got %v", got)
	}
	if got := failure.Both(empty); got.String() != failure.String() {
		t.Fatalf("expected failure Both Empty to equal failure, got %v", got)
	}
}

func TestCausePreservesSequentialAndParallelStructure(t *testing.T) {
	stop := errors.New("stop")
	cause := effect.FailCause("load").Then(
		effect.DieCause[string](effect.Defect{Value: "cleanup"}).Both(
			effect.InterruptCause[string](stop),
		),
	)

	if cause.Kind() != effect.CauseThen {
		t.Fatalf("expected Then root, got %s", cause.Kind())
	}
	if failures := cause.Failures(); !reflect.DeepEqual(failures, []string{"load"}) {
		t.Fatalf("unexpected failures: %#v", failures)
	}
	if defects := cause.Defects(); len(defects) != 1 || defects[0].Value != "cleanup" {
		t.Fatalf("unexpected defects: %#v", defects)
	}
	interruptions := cause.Interruptions()
	if len(interruptions) != 1 || !errors.Is(interruptions[0].Cause, stop) {
		t.Fatalf("unexpected interruptions: %#v", interruptions)
	}
	if !cause.ContainsDefect() || cause.IsInterruptedOnly() {
		t.Fatalf("unexpected cause predicates for %v", cause)
	}
}

func TestCauseMapFailurePreservesShape(t *testing.T) {
	cause := effect.FailCause(20).Both(effect.FailCause(22))
	mapped := cause.MapFailure(func(value int) string {
		return fmt.Sprintf("E%d", value)
	})

	if mapped.Kind() != effect.CauseBoth {
		t.Fatalf("expected Both root, got %s", mapped.Kind())
	}
	want := []string{"E20", "E22"}
	if got := mapped.Failures(); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestCauseRenderingIsDeterministic(t *testing.T) {
	cause := effect.FailCause("operation").Then(
		effect.DieCause[string](effect.Defect{Value: "cleanup"}),
	)
	want := "Then(\n  Fail(operation),\n  Die(cleanup)\n)"

	if got := cause.String(); got != want {
		t.Fatalf("unexpected rendering:\n%s", got)
	}
}

func TestRunPreservesContextCancellationCause(t *testing.T) {
	stop := errors.New("operator requested stop")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(stop)

	exit := effect.Run(ctx, effect.Unit{}, effect.Succeed[effect.Unit, string](42))
	cause, ok := exit.Cause()
	if !ok {
		t.Fatal("expected interruption")
	}
	interruption, ok := cause.Interruption()
	if !ok || !errors.Is(interruption.Cause, stop) {
		t.Fatalf("expected cancellation cause, got %#v", cause)
	}
}

func TestInterruptedOnlyIncludesComposedInterruptions(t *testing.T) {
	cause := effect.InterruptCause[string](context.Canceled).Both(
		effect.InterruptCause[string](context.DeadlineExceeded),
	)
	if !cause.IsInterruptedOnly() {
		t.Fatalf("expected interruption-only cause, got %v", cause)
	}
}

func TestCauseFoldDistinguishesThenFromBoth(t *testing.T) {
	cause := effect.FailCause("load").Then(
		effect.FailCause("left").Both(effect.FailCause("right")),
	)

	folded := cause.Fold(effect.CauseFolder[string, string]{
		Empty:        func() string { return "empty" },
		Failure:      func(value string) string { return value },
		Defect:       func(effect.Defect) string { return "defect" },
		Interruption: func(effect.Interruption) string { return "interrupt" },
		Then:         func(left, right string) string { return "then(" + left + "," + right + ")" },
		Both:         func(left, right string) string { return "both(" + left + "," + right + ")" },
	})

	if folded != "then(load,both(left,right))" {
		t.Fatalf("unexpected fold result: %s", folded)
	}
}
