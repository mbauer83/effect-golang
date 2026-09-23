package acceptance

import (
	"context"
	"sync"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
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
	// Both sequencing styles are held to the same assertions, so the claim that
	// they differ only in layout is checked rather than stated.
	for style, program := range checkout.Styles() {
		observer := &effecttest.EventRecorder{}
		runtime, err := effect.NewRuntime(effect.WithObserver(observer))
		if err != nil {
			t.Fatal(err)
		}

		exit := runtime.Run(context.Background(), catalog(), program("c-1", []string{"widget", "gasket"}))
		runtime.Close(context.Background())

		quote, ok := exit.Value()
		if !ok {
			t.Fatalf("%s: unexpected failure: %v", style, exit)
		}
		if quote.Customer != "Ada" || quote.Lines != 2 || quote.Total != 338 {
			t.Fatalf("%s: unexpected quote: %#v", style, quote)
		}
		assertObservedKinds(t, observer, map[effect.EventKind]bool{
			effect.EventSpanStarted: true,
			effect.EventSpanEnded:   true,
		})
	}
}

func TestCheckoutShortCircuitsWithoutRunningLaterSteps(t *testing.T) {
	for style, program := range checkout.Styles() {
		exit := effect.Run(context.Background(), catalog(), program("absent", []string{"widget"}))

		cause, failed := exit.Cause()
		if !failed {
			t.Fatalf("%s: expected an unknown customer to fail, got %v", style, exit)
		}
		failure, isLeaf := cause.Failure()
		if !isLeaf || failure.Step != "load-customer" {
			t.Fatalf("%s: expected the first step to fail, got %v", style, cause)
		}
	}
}

func TestCheckoutFailsOnAnUnpricedItemAfterTheEarlierStepsSucceeded(t *testing.T) {
	for style, program := range checkout.Styles() {
		exit := effect.Run(context.Background(), catalog(), program("c-2", []string{"widget", "flywheel"}))

		cause, failed := exit.Cause()
		if !failed {
			t.Fatalf("%s: expected an unpriced item to fail, got %v", style, exit)
		}
		failure, _ := cause.Failure()
		if failure.Step != "price" {
			t.Fatalf("%s: expected the pricing step to fail, got %#v", style, failure)
		}
	}
}

func TestCheckoutIsLazyAndSharesNoStateBetweenRuns(t *testing.T) {
	const runners = 16

	for style, build := range checkout.Styles() {
		program := build("c-1", []string{"widget", "gasket"})

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
				t.Fatalf("%s: run %d observed another run's state: total %d", style, index, total)
			}
		}
	}
}

func TestCheckoutStopsBeforeTheFirstStepWhenAlreadyCanceled(t *testing.T) {
	for style, program := range checkout.Styles() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		exit := effect.Run(ctx, catalog(), program("c-1", []string{"widget"}))
		cause, failed := exit.Cause()
		if !failed || !cause.HasInterruptsOnly() {
			t.Fatalf("%s: expected an interruption-only cause, got %v", style, exit)
		}
	}
}

func TestBothCheckoutStylesProduceIdenticalOutcomes(t *testing.T) {
	// Not the same assertions run twice: the same inputs compared against each
	// other, so a divergence in any path shows up as a mismatch rather than as
	// two separately plausible results.
	cases := map[string]struct {
		customer string
		items    []string
	}{
		"priced":           {"c-1", []string{"widget", "gasket"}},
		"no discount":      {"c-2", []string{"widget"}},
		"unknown customer": {"absent", []string{"widget"}},
		"empty basket":     {"c-1", nil},
		"unpriced item":    {"c-1", []string{"flywheel"}},
	}

	for name, input := range cases {
		direct := effect.Run(context.Background(), catalog(),
			checkout.Program(input.customer, input.items))
		chained := effect.Run(context.Background(), catalog(),
			checkout.ChainedProgram(input.customer, input.items))

		if direct.String() != chained.String() {
			t.Fatalf("%s: the styles disagree\n  direct:  %s\n  chained: %s",
				name, direct, chained)
		}
	}
}
