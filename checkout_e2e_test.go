package effect_test

import (
	"context"
	"sync"
	"testing"

	effect "github.com/mbauer83/effect-golang"
	"github.com/mbauer83/effect-golang/effecttest"
	"github.com/mbauer83/effect-golang/examples/checkout"
)

func catalog() checkout.Catalog {
	return checkout.Catalog{
		Customers: map[string]checkout.Customer{
			"c-1": {ID: "c-1", Name: "Ada", Discount: 10},
			"c-2": {ID: "c-2", Name: "Grace"},
		},
		Prices: map[string]int{"widget": 250, "gasket": 125},
	}
}

func TestCheckoutComposesDependentSteps(t *testing.T) {
	observer := &effecttest.RecordingObserver{}
	runtime, err := effect.NewRuntime(effect.WithObserver(observer))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())

	exit := runtime.Run(context.Background(), catalog(),
		checkout.Program("c-1", []string{"widget", "gasket"}))

	quote, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected failure: %v", exit)
	}
	if quote.Customer != "Ada" || quote.Lines != 2 || quote.Total != 338 {
		t.Fatalf("unexpected quote: %#v", quote)
	}
	assertObservedKinds(t, observer, map[effect.EventKind]bool{
		effect.EventSpanStarted: true,
		effect.EventSpanEnded:   true,
	})
}

func TestCheckoutShortCircuitsWithoutRunningLaterSteps(t *testing.T) {
	exit := effect.Run(context.Background(), catalog(),
		checkout.Program("absent", []string{"widget"}))

	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected an unknown customer to fail, got %v", exit)
	}
	failure, isLeaf := cause.Failure()
	if !isLeaf || failure.Step != "load-customer" {
		t.Fatalf("expected the first step to fail, got %v", cause)
	}
}

func TestCheckoutFailsOnAnUnpricedItemAfterTheEarlierStepsSucceeded(t *testing.T) {
	exit := effect.Run(context.Background(), catalog(),
		checkout.Program("c-2", []string{"widget", "flywheel"}))

	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected an unpriced item to fail, got %v", exit)
	}
	failure, _ := cause.Failure()
	if failure.Step != "price" {
		t.Fatalf("expected the pricing step to fail, got %#v", failure)
	}
}

func TestCheckoutIsLazyAndSharesNoStateBetweenRuns(t *testing.T) {
	const runners = 16
	program := checkout.Program("c-1", []string{"widget", "gasket"})

	var runs sync.WaitGroup
	totals := make([]int, runners)
	for index := range runners {
		runs.Go(func() {
			exit := effect.Run(context.Background(), catalog(), program)
			quote, _ := exit.Value()
			totals[index] = quote.Total
		})
	}
	runs.Wait()

	for index, total := range totals {
		if total != 338 {
			t.Fatalf("run %d observed another run's state: total %d", index, total)
		}
	}
}

func TestCheckoutStopsBeforeTheFirstStepWhenAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	exit := effect.Run(ctx, catalog(), checkout.Program("c-1", []string{"widget"}))
	cause, failed := exit.Cause()
	if !failed || !cause.IsInterruptedOnly() {
		t.Fatalf("expected an interruption-only cause, got %v", exit)
	}
}
