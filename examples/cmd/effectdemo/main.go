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
	"time"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/examples/checkout"
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
	program := filecopy.RetryingProgram(
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
	exit := runtime.Run(ctx, catalog, checkout.Program("c-1", []string{"widget", "gasket"}))
	if quote, ok := exit.Value(); ok {
		fmt.Printf("sequential workflow: %s owes %d for %d lines\n", quote.Customer, quote.Total, quote.Lines)
		return
	}
	report("sequential workflow", exit)
}

func runDiagnostics(runtime *effect.Runtime, ctx context.Context) {
	diagnosis := diagnostics.Diagnose(runtime.Run(ctx, effect.Unit{}, diagnostics.Program()))
	fmt.Printf("failure diagnostics (%s):\n%s\n", diagnosis.Status, diagnosis.Rendered)
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
