package checkout

// The same workflow written in direct style, so the two can be compared on real
// code rather than on a snippet -- and so an end-to-end test can assert that
// they agree on every path.
//
// The difference is the whole point: this version needs no state type and no
// transition per step, because each Await returns the value the next line uses.
// What it costs is described in docs/reference/direct.md.

import (
	"log/slog"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/experimental/direct"
)

// DirectProgram prices a basket for a customer, in direct style.
//
// It is the same workflow as Program: the same steps, the same failures, the
// same laziness. Only the sequencing differs.
func DirectProgram(customerID string, items []string) workflowEffect[Quote] {
	return direct.Run(func(do *direct.Do[Catalog, CheckoutError]) Quote {
		customer := do.Await(loadCustomer(customerID))
		basket := do.Await(loadBasket(customer, items))
		return do.Await(price(customer, basket))
	}).
		WithName("checkout").
		WithSpan("checkout", slog.String("customer", customerID))
}

// styles names the two ways this scenario is written, so a test can run both
// through the same assertions instead of duplicating them.
func Styles() map[string]func(string, []string) effect.Effect[Catalog, CheckoutError, Quote] {
	return map[string]func(string, []string) effect.Effect[Catalog, CheckoutError, Quote]{
		"workflow": Program,
		"direct":   DirectProgram,
	}
}
