package unit

import (
	"context"
	"reflect"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
)

type queueProgram[A any] = effect.Effect[effect.Unit, string, A]

var queueOperations = effect.For[effect.Unit, string]()

// offerAll enqueues every value in order, reporting which offers were accepted.
func offerAll(queue effect.Queue[int], values ...int) queueProgram[[]bool] {
	return effect.ForEach(values, func(value int) queueProgram[bool] {
		return queueOperations.Offer(queue, value)
	})
}

func TestQueuePreservesOfferOrder(t *testing.T) {
	program := queueOperations.Queue[int](8, effect.SuspendWhenFull).FlatMap(
		func(queue effect.Queue[int]) queueProgram[[]int] {
			return offerAll(queue, 1, 2, 3).
				AndThen(queueOperations.TakeUpTo(queue, 8))
		},
	)

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if got, ok := exit.Value(); !ok || !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("expected offer order preserved, got %v", exit)
	}
}

func TestSuspendWhenFullAppliesBackpressure(t *testing.T) {
	program := effect.Scoped(func(effect.Scope) queueProgram[string] {
		return queueOperations.Queue[int](1, effect.SuspendWhenFull).FlatMap(
			func(queue effect.Queue[int]) queueProgram[string] {
				effect.Run(context.Background(), effect.Unit{},
					queueOperations.Offer(queue, 1))

				return queueOperations.Fork(queueOperations.Offer(queue, 2).As("offered")).
					FlatMap(func(producer forkedFiber) queueProgram[string] {
						if _, done := producer.Poll(); done {
							t.Error("expected the producer to wait for room")
						}
						// Taking frees the slot the producer is waiting for.
						effect.Run(context.Background(), effect.Unit{}, queueOperations.Take(queue))
						<-producer.Done()
						if queue.Size() != 1 {
							t.Errorf("expected the waiting value admitted, size is %d", queue.Size())
						}
						return queueOperations.Join(producer)
					})
			},
		)
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if value, ok := exit.Value(); !ok || value != "offered" {
		t.Fatalf("unexpected exit: %v", exit)
	}
}

func TestFullQueueStrategiesDifferInWhatTheyKeep(t *testing.T) {
	strategies := map[effect.WhenFull][]int{
		effect.DropNewestWhenFull: {1, 2},
		effect.DropOldestWhenFull: {2, 3},
	}

	for whenFull, want := range strategies {
		program := queueOperations.Queue[int](2, whenFull).FlatMap(
			func(queue effect.Queue[int]) queueProgram[[]int] {
				return offerAll(queue, 1, 2, 3).
					AndThen(queueOperations.TakeUpTo(queue, 8))
			},
		)

		exit := effect.Run(context.Background(), effect.Unit{}, program)
		got, ok := exit.Value()
		if !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("strategy %d: expected %v, got %v", whenFull, want, exit)
		}
	}
}

func TestDroppingQueueReportsThatItDeclinedAnOffer(t *testing.T) {
	program := queueOperations.Queue[int](1, effect.DropNewestWhenFull).FlatMap(
		func(queue effect.Queue[int]) queueProgram[[]bool] {
			return offerAll(queue, 1, 2)
		},
	)

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if got, ok := exit.Value(); !ok || !reflect.DeepEqual(got, []bool{true, false}) {
		t.Fatalf("expected the second offer declined, got %v", exit)
	}
}

func TestUnboundedQueueNeverRefuses(t *testing.T) {
	values := make([]int, 500)
	program := queueOperations.Queue[int](1, effect.SuspendWhenFull).FlatMap(
		func(effect.Queue[int]) queueProgram[[]bool] {
			return effect.WidenError[string](effect.NewUnboundedQueue[effect.Unit, int]()).
				FlatMap(func(unbounded effect.Queue[int]) queueProgram[[]bool] {
					return offerAll(unbounded, values...)
				})
		},
	)

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	accepted, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected failure: %v", exit)
	}
	for index, admitted := range accepted {
		if !admitted {
			t.Fatalf("offer %d was refused by an unbounded queue", index)
		}
	}
}
