package acceptance

// A complete program configured from several places by several consumers.
//
// The unit tests state what a description reads and what a source answers.
// What this adds is the whole path: a runtime told where to read, a layer
// built from two components' descriptions, a program that requires the result
// and never mentions a source, and the two failures a deployment actually
// hits -- a value nobody supplied, and a value supplied wrongly.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effect/config"
	"github.com/mbauer83/effect-golang/examples/configured"
)

// deployment is what a deployment supplies, in the environment's own spelling,
// over the defaults the program ships with.
func deployment(entries ...string) config.Source {
	return config.Sources(config.EnvironmentOf(entries...), configured.Defaults())
}

func runConfigProgram(t *testing.T, source config.Source) effect.Exit[configured.Refusal, string] {
	t.Helper()
	runtime, err := effect.NewRuntime(effect.WithConfigSource(source))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	return runtime.Run(context.Background(), effect.Unit{}, configured.Program())
}

// refusalOf is the one typed failure an exit carries, which is what a
// program's settings failing looks like: a ConfigError adapted once, at the
// layer, into this program's own vocabulary.
func refusalOf(
	exit effect.Exit[configured.Refusal, string],
) (configured.Refusal, bool) {
	cause, failed := exit.Cause()
	if !failed {
		return configured.Refusal{}, false
	}
	refusals := cause.Failures()
	if len(refusals) != 1 {
		return configured.Refusal{}, false
	}
	return refusals[0], true
}

func TestAProgramReadsItsSettingsFromTheRuntimeItRunsIn(t *testing.T) {
	exit := runConfigProgram(t, deployment(
		"DB_HOST=primary.internal",
		"DB_PASSWORD=hunter2",
		"DB_TIMEOUT=250ms",
		"DB_LIMITS_READ=100",
		"DB_LIMITS_WRITE=25",
		"MAIL_SENDER=service@example.com",
		"MAIL_RELAYS=relay-one:25,relay-two:25",
		"MAIL_RETRIES=5",
	))

	said, succeeded := exit.Value()
	if !succeeded {
		t.Fatalf("expected the program to run, got %v", exit)
	}
	for _, want := range []string{
		"store primary.internal timeout 250ms",
		"limits read=100 write=25",
		"mail from service@example.com via relay-one:25 then relay-two:25 retrying 5",
	} {
		if !strings.Contains(said, want) {
			t.Fatalf("expected %q in\n%s", want, said)
		}
	}
	// The secret was read, used, and cannot be printed.
	if !strings.Contains(said, "password <redacted>") {
		t.Fatalf("expected the password redacted, got\n%s", said)
	}
	if strings.Contains(said, "hunter2") {
		t.Fatalf("the secret leaked into output:\n%s", said)
	}
}

func TestWhatTheDeploymentDidNotSupplyComesFromTheDefaultsBeneathIt(t *testing.T) {
	// Only the mail sender is supplied. Everything else comes from the
	// program's own defaults source or from the defaults on the descriptions,
	// and the difference is invisible to the program.
	exit := runConfigProgram(t, deployment("MAIL_SENDER=service@example.com"))

	said, succeeded := exit.Value()
	if !succeeded {
		t.Fatalf("expected the defaults to be enough, got %v", exit)
	}
	if !strings.Contains(said, "store localhost timeout 5s") {
		t.Fatalf("expected the shipped defaults, got\n%s", said)
	}
	if !strings.Contains(said, "limits none configured") {
		t.Fatalf("expected no limits, got\n%s", said)
	}
	if !strings.Contains(said, "retrying 3") {
		t.Fatalf("expected the described default, got\n%s", said)
	}
}

func TestEverySettingADeploymentIsMissingIsReportedOnce(t *testing.T) {
	// No defaults source at all, so the two values that have no default are
	// both missing. A program that reported one of them would be restarted
	// once per key.
	exit := runConfigProgram(t, config.EnvironmentOf())

	failure, failed := refusalOf(exit)
	if !failed {
		t.Fatalf("expected the program to refuse to start, got %v", exit)
	}
	for _, want := range []string{"db.host", "db.password", "mail.sender", "mail.relays"} {
		if !strings.Contains(failure.Because, want) {
			t.Fatalf("expected %q reported, got %q", want, failure.Because)
		}
	}
}

func TestASettingSuppliedWrongIsAFailureAndNotADefault(t *testing.T) {
	exit := runConfigProgram(t, deployment(
		"MAIL_SENDER=service@example.com",
		"DB_TIMEOUT=half a minute",
	))

	failure, failed := refusalOf(exit)
	if !failed {
		t.Fatalf("expected the unreadable duration to stop the program, got %v", exit)
	}
	if !strings.Contains(failure.Because, "db.timeout is not a duration") {
		t.Fatalf("expected the timeout reported, got %q", failure.Because)
	}
}

func TestOneComponentReadsFromASourceTheRestOfTheProgramDoesNot(t *testing.T) {
	// The n-to-m case. The program reads the environment; the plugin reads a
	// document, mounted at its own place inside it.
	document := config.FromMap(map[string]string{
		"plugins.reporting.every": "30s",
		"plugins.billing.every":   "1h",
	})
	runtime, err := effect.NewRuntime(effect.WithConfigSource(
		deployment("MAIL_SENDER=service@example.com")))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())

	program := configured.ReadReportSchedule(document, "reporting")
	exit := runtime.Run(context.Background(), configured.Service{}, program)
	reporting, succeeded := exit.Value()
	if !succeeded {
		t.Fatalf("expected the plugin's own source to answer, got %v", exit)
	}
	if !reporting.Enabled || reporting.Every != 30*time.Second {
		t.Fatalf("read %+v", reporting)
	}

	// A plugin the document says nothing about is off, and that is a value
	// rather than an absence the program has to remember to check.
	quiet := configured.ReadReportSchedule(document, "search")
	off, succeeded := runtime.Run(context.Background(), configured.Service{}, quiet).Value()
	if !succeeded || off.Enabled {
		t.Fatalf("expected an unconfigured plugin to be off, got %+v", off)
	}

	// And the surrounding program still reads the environment, unaffected by
	// what the plugin read.
	if _, ran := runtime.Run(context.Background(), effect.Unit{},
		configured.Program()).Value(); !ran {
		t.Fatal("expected the program's own source to be untouched")
	}
}

func TestADependentReadSequencesWithTheEffectsOwnFlatMap(t *testing.T) {
	// Which store to use is itself configured. Accumulating composition
	// belongs to a description; sequencing belongs to the runtime, so this is
	// FlatMap and not a second mechanism.
	runtime, err := effect.NewRuntime(effect.WithConfigSource(config.FromMap(map[string]string{
		"store":               "replica",
		"replica.db.host":     "replica.internal",
		"replica.db.password": "hunter2",
	})))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())

	exit := runtime.Run(context.Background(), effect.Unit{},
		configured.StoreFor(config.Text("store")))
	store, succeeded := exit.Value()
	if !succeeded {
		t.Fatalf("expected the chosen store to be read, got %v", exit)
	}
	if store.Host != "replica.internal" || store.Port != 5432 {
		t.Fatalf("read %+v", store)
	}
}

func TestAProgramCanSayWhatItNeedsWithoutReadingAnything(t *testing.T) {
	needed := configured.Requirements()

	for _, want := range []string{
		"db.host",
		"db.password",
		"a secret",
		"required",
		"the address of the primary",
		"5432",
		"mail.retries",
	} {
		if !strings.Contains(needed, want) {
			t.Fatalf("expected %q in the printed expectations:\n%s", want, needed)
		}
	}
	// A description holds no values, so this cannot print one.
	if strings.Contains(needed, "hunter2") || strings.Contains(needed, "localhost") {
		t.Fatalf("expectations printed a value:\n%s", needed)
	}
}
