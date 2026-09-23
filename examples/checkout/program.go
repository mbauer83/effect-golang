// Package checkout is a complete program showing a longer sequential workflow
// whose later steps depend on several earlier results.
//
// It is written in direct style, which reads in the order it runs, and once
// more as the FlatMap chain it describes, so an end-to-end test can assert the
// two agree on every path.
package checkout

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mbauer83/effect-golang/effect"
)

// Catalog is the workflow's requirement. Keeping it in R rather than reaching
// for a global keeps the program's dependencies visible in its type.
type Catalog struct {
	Customers map[string]Customer
	Prices    map[string]int
}

// Customer is a known buyer.
type Customer struct {
	ID       string
	Name     string
	Discount int
}

// Basket is what a customer intends to buy.
type Basket struct {
	Items []string
}

// Quote is the priced outcome of a checkout.
type Quote struct {
	Customer string
	Lines    int
	Total    int
}

// CheckoutError is the workflow's expected failure type.
type CheckoutError struct {
	Step   string
	Detail string
}

func (failure CheckoutError) Error() string {
	return fmt.Sprintf("%s: %s", failure.Step, failure.Detail)
}

type workflowEffect[A any] = effect.Effect[Catalog, CheckoutError, A]

// Program prices a basket for a customer.
//
// Every step is lazy: nothing is looked up, logged or priced until the effect
// is interpreted, and the body runs afresh for each interpretation, so a retry
// or a concurrent run never inherits another run's partial state.
func Program(customerID string, items []string) workflowEffect[Quote] {
	return effect.Gen(func(do *effect.Do[Catalog, CheckoutError]) Quote {
		customer := do.Await(loadCustomer(customerID))
		basket := do.Await(loadBasket(customer, items))
		return do.Await(price(customer, basket))
	}).
		WithName("checkout").
		WithSpan("checkout", slog.String("customer", customerID))
}

func loadCustomer(customerID string) workflowEffect[Customer] {
	operations := effect.For[Catalog, CheckoutError]()
	return operations.From(func(_ context.Context, catalog Catalog) effect.Exit[CheckoutError, Customer] {
		customer, known := catalog.Customers[customerID]
		if !known {
			return effect.ExitFailure[CheckoutError, Customer](CheckoutError{
				Step:   "load-customer",
				Detail: "unknown customer " + customerID,
			})
		}
		return effect.ExitSuccess[CheckoutError](customer)
	}).WithName("load-customer")
}

func loadBasket(customer Customer, items []string) workflowEffect[Basket] {
	operations := effect.For[Catalog, CheckoutError]()
	if len(items) == 0 {
		return operations.Fail[Basket](CheckoutError{
			Step:   "load-basket",
			Detail: "basket for " + customer.Name + " is empty",
		})
	}
	return operations.Succeed(Basket{Items: items}).WithName("load-basket")
}

func price(customer Customer, basket Basket) workflowEffect[Quote] {
	return effect.ForEach(basket.Items, lineTotal).
		Map(func(lines []int) Quote {
			return quoteFor(customer, lines)
		}).
		WithName("price")
}

func quoteFor(customer Customer, lines []int) Quote {
	total := 0
	for _, line := range lines {
		total += line
	}
	return Quote{
		Customer: customer.Name,
		Lines:    len(lines),
		Total:    total - total*customer.Discount/100,
	}
}

func lineTotal(item string) workflowEffect[int] {
	operations := effect.For[Catalog, CheckoutError]()
	return operations.From(func(_ context.Context, catalog Catalog) effect.Exit[CheckoutError, int] {
		amount, listed := catalog.Prices[item]
		if !listed {
			return effect.ExitFailure[CheckoutError, int](CheckoutError{
				Step:   "price",
				Detail: "no price for " + item,
			})
		}
		return effect.ExitSuccess[CheckoutError](amount)
	})
}
