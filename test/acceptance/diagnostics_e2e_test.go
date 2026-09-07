package acceptance

import (
	"context"
	"errors"
	"strings"
	"testing"

	effect "github.com/mbauer83/effect-golang"
	"github.com/mbauer83/effect-golang/examples/diagnostics"
)

// The complete cause of this scenario is four independent facts: one stage's
// typed rejection, that stage's cleanup defect, the other stage's panic, and
// that stage's cleanup defect. A scalar error model keeps one of them.
const wantRenderedCause = `Both(
  Then(
    Fail(validate: record is not well formed),
    Die(validate: cleanup failed)
  ),
  Then(
    Die(index out of range in stage 'transform'),
    Die(transform: cleanup failed)
  )
)`

func TestFailureDiagnosticsPreservesEveryFact(t *testing.T) {
	exit := effect.Run(context.Background(), effect.Unit{}, diagnostics.Program())
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected the scenario to fail, got %v", exit)
	}

	if cause.Kind() != effect.CauseBoth {
		t.Fatalf("expected two independent branches, got %v", cause.Kind())
	}
	failures := cause.Failures()
	if len(failures) != 1 || failures[0].Stage != "validate" {
		t.Fatalf("expected the typed rejection to survive, got %#v", failures)
	}
	if defects := cause.Defects(); len(defects) != 3 {
		t.Fatalf("expected one panic and two cleanup defects, got %d", len(defects))
	}
	for _, defect := range cause.Defects() {
		if wrapped, ok := defect.Value.(error); ok && !errors.Is(wrapped, diagnostics.ErrCleanupFailed) {
			t.Fatalf("expected a cleanup error to keep its wrapping, got %v", wrapped)
		}
	}
	if cause.IsFailureOnly() || cause.IsInterruptedOnly() || !cause.ContainsDefect() {
		t.Fatalf("unexpected cause predicates for %v", cause)
	}
}

func TestFailureDiagnosticsRenderingIsDeterministic(t *testing.T) {
	// Rendered twice from two independent interpretations: the shape must not
	// depend on which branch happened to finish first.
	for range 2 {
		diagnosis := diagnostics.Diagnose(
			effect.Run(context.Background(), effect.Unit{}, diagnostics.Program()),
		)
		if diagnosis.Rendered != wantRenderedCause {
			t.Fatalf("unexpected rendering:\n%s", diagnosis.Rendered)
		}
		if diagnosis.Status != effect.EventStatusDefect {
			t.Fatalf("expected a defect status, got %q", diagnosis.Status)
		}
	}
}

func TestFailureDiagnosticsReportMirrorsTheRendering(t *testing.T) {
	diagnosis := diagnostics.Diagnose(
		effect.Run(context.Background(), effect.Unit{}, diagnostics.Program()),
	)

	report := diagnosis.Report
	if report.Kind != effect.CauseBoth || len(report.Children) != 2 {
		t.Fatalf("expected a Both with two children, got %#v", report)
	}
	rejected := report.Children[0]
	if rejected.Kind != effect.CauseThen || len(rejected.Children) != 2 {
		t.Fatalf("expected the rejecting branch to be a Then, got %#v", rejected)
	}
	if rejected.Children[0].Kind != effect.CauseFailure {
		t.Fatalf("expected a typed failure first, got %#v", rejected.Children[0])
	}
	panicked := report.Children[1].Children[0]
	if panicked.Kind != effect.CauseDefect || !strings.Contains(panicked.Stack, "goroutine") {
		t.Fatalf("expected the panic's stack in the structured report, got %#v", panicked)
	}
}

func TestFailureDiagnosticsProgramStaysReusable(t *testing.T) {
	// The scenario allocates its rendezvous per interpretation, so one Effect
	// value can be run repeatedly without the second run deadlocking on state
	// the first run consumed.
	program := diagnostics.Program()
	for attempt := range 3 {
		if exit := effect.Run(context.Background(), effect.Unit{}, program); exit.IsSuccess() {
			t.Fatalf("attempt %d unexpectedly succeeded", attempt)
		}
	}
}
