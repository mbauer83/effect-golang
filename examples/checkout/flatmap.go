package checkout

// The same workflow written as the FlatMap chain it describes, so the two can
// be compared on real code rather than on a snippet -- and so an end-to-end
// test can assert that they agree on every path.
//
// The difference is the whole point: each step nests inside the one before
// it, so the last thing to happen is indented deepest.

import (
	"log/slog"

	"github.com/mbauer83/effect-golang/effect"
)

// FlatMapProgram prices a basket for a customer, as a FlatMap chain.
//
// It is the same workflow as Program: the same steps, the same failures, the
// same laziness. Only the sequencing differs.
func FlatMapProgram(customerID string, items []string) workflowEffect[Quote] {
	return effect.Suspend(func() workflowEffect[Quote] {
		return loadCustomer(customerID).FlatMap(func(customer Customer) workflowEffect[Quote] {
			return loadBasket(customer, items).FlatMap(func(basket Basket) workflowEffect[Quote] {
				return price(customer, basket)
			})
		})
	}).
		WithName("checkout").
		WithSpan("checkout", slog.String("customer", customerID))
}

// Styles names the two ways this scenario is written, so a test can run both
// through the same assertions instead of duplicating them.
func Styles() map[string]func(string, []string) effect.Effect[Catalog, CheckoutError, Quote] {
	return map[string]func(string, []string) effect.Effect[Catalog, CheckoutError, Quote]{
		"direct":  Program,
		"flatmap": FlatMapProgram,
	}
}
