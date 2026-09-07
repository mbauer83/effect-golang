// Package checkout is a complete program showing a longer sequential workflow
// whose later steps depend on several earlier results.
//
// Go has no do-notation and no resumable generators, so a dependent chain is
// ultimately a chain of binds. The typed state builder does not pretend
// otherwise: it packages successive FlatMap steps around one caller-declared
// state type so the source stays visually flat while laziness, cancellation and
// the three typed channels behave exactly as they do for the core operators.
package checkout

import (
	"context"
	"fmt"
	"log/slog"

	effect "github.com/mbauer83/effect-golang"
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

// state is the workflow's caller-declared state. TypeScript can grow a record
// type after every bind; Go cannot, so one explicit state type carries the
// whole workflow and each transition returns a new value rather than mutating
// shared data.
type state struct {
	customer Customer
	basket   Basket
	quote    Quote
}

type workflowEffect[A any] = effect.Effect[Catalog, CheckoutError, A]

// Program prices a basket for a customer.
//
// Every step is lazy: nothing is looked up, logged or priced until the effect
// is interpreted, and the state is created afresh for each interpretation, so a
// retry or a concurrent run never inherits another run's partial state.
func Program(customerID string, items []string) workflowEffect[Quote] {
	operations := effect.For[Catalog, CheckoutError]()
	return operations.Do(func() state { return state{} }).
		Bind(
			func(state) workflowEffect[Customer] { return loadCustomer(customerID) },
			func(current state, customer Customer) state {
				current.customer = customer
				return current
			},
		).
		Bind(
			func(current state) workflowEffect[Basket] {
				return loadBasket(current.customer, items)
			},
			func(current state, basket Basket) state {
				current.basket = basket
				return current
			},
		).
		Bind(
			func(current state) workflowEffect[Quote] {
				return price(current.customer, current.basket)
			},
			func(current state, quote Quote) state {
				current.quote = quote
				return current
			},
		).
		Yield(func(current state) Quote { return current.quote }).
		Named("checkout").
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
	}).Named("load-customer")
}

func loadBasket(customer Customer, items []string) workflowEffect[Basket] {
	operations := effect.For[Catalog, CheckoutError]()
	if len(items) == 0 {
		return operations.Fail[Basket](CheckoutError{
			Step:   "load-basket",
			Detail: "basket for " + customer.Name + " is empty",
		})
	}
	return operations.Succeed(Basket{Items: items}).Named("load-basket")
}

func price(customer Customer, basket Basket) workflowEffect[Quote] {
	return effect.ForEach(basket.Items, lineTotal).
		Map(func(lines []int) Quote {
			return quoteFor(customer, lines)
		}).
		Named("price")
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
