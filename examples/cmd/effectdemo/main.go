// Command effectdemo runs every example scenario against live capabilities.
//
// It exists so the examples are demonstrably runnable programs and not only
// test fixtures. Each scenario is also composed by an end-to-end test, so the
// two cannot drift apart.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effect/config"
	"github.com/mbauer83/effect-golang/examples/checkout"
	"github.com/mbauer83/effect-golang/examples/configured"
	"github.com/mbauer83/effect-golang/examples/diagnostics"
	"github.com/mbauer83/effect-golang/examples/fanout"
	"github.com/mbauer83/effect-golang/examples/filecopy"
	"github.com/mbauer83/effect-golang/examples/parallelimport"
	"github.com/mbauer83/effect-golang/examples/pipeline"
)

func main() {
	workspace, err := os.MkdirTemp("", "effectdemo")
	if err != nil {
		fail(err)
	}
	defer os.RemoveAll(workspace)

	runtime, err := effect.NewRuntime(effect.WithDebugTracking())
	if err != nil {
		fail(err)
	}
	ctx := context.Background()
	defer reportShutdown(runtime, ctx)

	if err := seed(workspace); err != nil {
		fail(err)
	}
	runFileCopy(runtime, ctx, workspace)
	runParallelImport(runtime, ctx, workspace)
	runPipeline(runtime, ctx, workspace)
	runFanout(runtime, ctx, workspace)
	runCheckout(runtime, ctx)
	runDiagnostics(runtime, ctx)
	runConfigDemo(ctx)
}

func seed(workspace string) error {
	sources := map[string]string{
		"alpha.txt":   "alpha source\n",
		"beta.txt":    "beta source with more words\n",
		"records.txt": "first record\nsecond record here\nthird\n",
		"events.log":  "INFO started\nWARN slow response\nINFO handled\nWARN retrying\nINFO done\n",
	}
	for name, content := range sources {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func runFileCopy(runtime *effect.Runtime, ctx context.Context, workspace string) {
	policy := effect.Recurs[effect.IOError](2).MapOutput(func(uint64) time.Duration { return 0 })
	program := filecopy.ProgramWithRetry(
		filepath.Join(workspace, "alpha.txt"),
		filepath.Join(workspace, "alpha.upper.txt"),
		policy,
	)
	report("resilient file import", runtime.Run(ctx, effect.Unit{}, program))
}

func runParallelImport(runtime *effect.Runtime, ctx context.Context, workspace string) {
	program := parallelimport.Program(
		[]string{
			filepath.Join(workspace, "alpha.txt"),
			filepath.Join(workspace, "beta.txt"),
		},
		filepath.Join(workspace, "import.lock"),
		2,
	)
	exit := runtime.Run(ctx, effect.Unit{}, program)
	if summary, ok := exit.Value(); ok {
		fmt.Printf("scoped parallel work: %s (%d bytes)\n", parallelimport.Describe(summary), summary.Bytes)
		return
	}
	report("scoped parallel work", exit)
}

func runPipeline(runtime *effect.Runtime, ctx context.Context, workspace string) {
	program := pipeline.Program(filepath.Join(workspace, "records.txt"), 4)
	exit := runtime.Run(ctx, effect.Unit{}, program)
	if summary, ok := exit.Value(); ok {
		fmt.Printf("native channel bridge: %d records, %d words\n", summary.Records, summary.Words)
		return
	}
	report("native channel bridge", exit)
}

func runFanout(runtime *effect.Runtime, ctx context.Context, workspace string) {
	exit := runtime.Run(ctx, effect.Unit{}, fanout.Program(filepath.Join(workspace, "events.log"), 3))
	if report, ok := exit.Value(); ok {
		fmt.Printf("fan-out pipeline: %s, %d records, %d warnings\n",
			report.Label, report.Records, report.Warnings)
		return
	}
	report("fan-out pipeline", exit)
}

func runCheckout(runtime *effect.Runtime, ctx context.Context) {
	catalog := checkout.Catalog{
		Customers: map[string]checkout.Customer{
			"c-1": {ID: "c-1", Name: "Ada", Discount: 10},
		},
		Prices: map[string]int{"widget": 250, "gasket": 125},
	}
	for _, style := range []string{"workflow", "direct"} {
		exit := runtime.Run(ctx, catalog, checkout.Styles()[style]("c-1", []string{"widget", "gasket"}))
		quote, ok := exit.Value()
		if !ok {
			report("sequential workflow ("+style+")", exit)
			continue
		}
		fmt.Printf("sequential workflow (%s): %s owes %d for %d lines\n",
			style, quote.Customer, quote.Total, quote.Lines)
	}
}

func runDiagnostics(runtime *effect.Runtime, ctx context.Context) {
	diagnosis := diagnostics.Diagnose(runtime.Run(ctx, effect.Unit{}, diagnostics.Program()))
	fmt.Printf("failure diagnostics (%s):\n%s\n", diagnosis.Status, diagnosis.Text)
}

// report prints an outcome the scenario did not expect. Exit renders both a
// success and a complete cause tree, so a failure needs no special handling.
func report[E, A any](scenario string, exit effect.Exit[E, A]) {
	fmt.Printf("%s: %s\n", scenario, exit)
}

func reportShutdown(runtime *effect.Runtime, ctx context.Context) {
	remaining := runtime.LiveWork()
	cleanup := runtime.Close(ctx)
	fmt.Printf("shutdown: %d fibers and %d resources still owned at Close\n",
		remaining.Fibers, remaining.Resources)
	if !cleanup.IsEmpty() {
		fmt.Printf("shutdown cleanup: %s\n", cleanup)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "effectdemo:", err)
	os.Exit(1)
}

// runConfigDemo needs a runtime of its own, because what it demonstrates is a
// runtime told where to read a program's settings.
//
// The deployment here is a fixed environment rather than this process's, so
// the demo says the same thing on every machine. A real program passes
// config.Environment(), which is also the default.
func runConfigDemo(ctx context.Context) {
	deployment := config.Sources(
		config.EnvironmentOf(
			"DB_HOST=primary.internal",
			"DB_PASSWORD=hunter2",
			"DB_LIMITS_READ=100",
			"MAIL_SENDER=service@example.com",
			"MAIL_RELAYS=relay-one:25,relay-two:25",
		),
		configured.Defaults(),
	)
	runtime, err := effect.NewRuntime(effect.WithConfigSource(deployment))
	if err != nil {
		fail(err)
	}
	defer runtime.Close(ctx)

	exit := runtime.Run(ctx, effect.Unit{}, configured.Program())
	text, ok := exit.Value()
	if !ok {
		report("described settings", exit)
		return
	}
	fmt.Printf("described settings:\n%s\n", indent(text))

	// The same program, with nothing supplied: every setting that has no
	// default is reported at once rather than one restart at a time.
	bare, err := effect.NewRuntime(effect.WithConfigSource(config.EnvironmentOf()))
	if err != nil {
		fail(err)
	}
	defer bare.Close(ctx)
	if cause, failed := bare.Run(ctx, effect.Unit{}, configured.Program()).Cause(); failed {
		for _, refusal := range cause.Failures() {
			fmt.Printf("unconfigured deployment: %s\n", refusal.Because)
		}
	}

	fmt.Printf("what it needs:\n%s", configured.Requirements())
}

func indent(text string) string {
	lines := strings.Split(text, "\n")
	for at, line := range lines {
		lines[at] = "  " + line
	}
	return strings.Join(lines, "\n")
}
