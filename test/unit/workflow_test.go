package unit

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
)

type pricingState struct {
	customerID int
	discount   int
	price      int
}

func TestWorkflowFlattensDependentBindingsAndRemainsLazy(t *testing.T) {
	var factories atomic.Int32
	operations := effect.For[effect.Unit, string]()
	program := operations.Do(func() pricingState {
		factories.Add(1)
		return pricingState{customerID: 7}
	}).Bind(
		func(state pricingState) effect.Effect[effect.Unit, string, int] {
			return operations.Succeed(state.customerID * 2)
		},
		func(state pricingState, discount int) pricingState {
			state.discount = discount
			return state
		},
	).Bind(
		func(state pricingState) effect.Effect[effect.Unit, string, int] {
			return operations.Succeed(100 - state.discount)
		},
		func(state pricingState, price int) pricingState {
			state.price = price
			return state
		},
	).Yield(func(state pricingState) int {
		return state.price
	})

	if factories.Load() != 0 {
		t.Fatal("workflow factory ran during construction")
	}
	for range 2 {
		exit := effect.Run(context.Background(), effect.Unit{}, program)
		value, ok := exit.Value()
		if !ok || value != 86 {
			t.Fatalf("unexpected workflow result: %+v", exit)
		}
	}
	if factories.Load() != 2 {
		t.Fatalf("expected fresh state per run, got %d factories", factories.Load())
	}
}

func TestWorkflowShortCircuitsWithoutUpdatingLaterState(t *testing.T) {
	var laterSteps atomic.Int32
	operations := effect.For[effect.Unit, string]()
	program := operations.Do(func() pricingState {
		return pricingState{}
	}).Bind(
		func(pricingState) effect.Effect[effect.Unit, string, int] {
			return operations.Fail[int]("no quote")
		},
		func(state pricingState, price int) pricingState {
			state.price = price
			return state
		},
	).Bind(
		func(pricingState) effect.Effect[effect.Unit, string, int] {
			laterSteps.Add(1)
			return operations.Succeed(1)
		},
		func(state pricingState, value int) pricingState {
			return state
		},
	).Yield(func(state pricingState) int {
		return state.price
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if !exit.IsFailure() || laterSteps.Load() != 0 {
		t.Fatalf("workflow did not short-circuit: exit=%+v later=%d", exit, laterSteps.Load())
	}
}
