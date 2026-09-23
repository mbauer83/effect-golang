package unit

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effecttest"
)

// A queue's lifetime: when it stops accepting work, who is released, what
// survives, and whether anything is lost under concurrent use.

func TestQueueShutdownDrainsThenReportsFinished(t *testing.T) {
	// This is the channel semantic the queue deliberately keeps: a consumer
	// finishes the backlog before it learns the producer is done.
	program := queueOperations.Queue[int](8, effect.SuspendWhenFull).FlatMap(
		func(queue effect.Queue[int]) queueProgram[[]effect.Receive[int]] {
			return offerAll(queue, 7, 8).
				AndThen(queueOperations.WidenError(queue.Shutdown[effect.Unit]())).
				AndThen(effect.ForEach([]int{0, 1, 2}, func(int) queueProgram[effect.Receive[int]] {
					return queueOperations.Take(queue)
				}))
		},
	)

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	received, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected failure: %v", exit)
	}
	want := []effect.Receive[int]{{Value: 7, OK: true}, {Value: 8, OK: true}, {Value: 0, OK: false}}
	if !reflect.DeepEqual(received, want) {
		t.Fatalf("expected the backlog then a finished signal, got %#v", received)
	}
}

func TestQueueShutdownIsSafeFromTheConsumerSideAndIdempotent(t *testing.T) {
	// Closing a channel from the consumer side is unsafe by construction; this
	// is the difference that justifies the type.
	tracker := &effecttest.Tracker{}

	program := effect.Scoped(func(effect.Scope) queueProgram[string] {
		return queueOperations.Queue[int](1, effect.SuspendWhenFull).FlatMap(
			func(queue effect.Queue[int]) queueProgram[string] {
				blocked := queueOperations.Take(queue).Map(func(received effect.Receive[int]) string {
					if received.OK {
						tracker.Record("received")
					} else {
						tracker.Record("finished")
					}
					return "consumed"
				})
				return queueOperations.Fork(blocked).FlatMap(func(consumer programFiber) queueProgram[string] {
					shutdown := queue.Shutdown[effect.Unit]()
					effect.Run(context.Background(), effect.Unit{}, shutdown)
					effect.Run(context.Background(), effect.Unit{}, shutdown)
					return queueOperations.Join(consumer)
				})
			},
		)
	})

	if exit := effect.Run(context.Background(), effect.Unit{}, program); exit.IsFailure() {
		t.Fatalf("unexpected failure: %v", exit)
	}
	if got := tracker.Events(); !reflect.DeepEqual(got, []string{"finished"}) {
		t.Fatalf("expected the blocked consumer released exactly once, got %v", got)
	}
}

func TestScopedQueueIsShutDownWhenItsScopeCloses(t *testing.T) {
	// A consumer forked inside the scope is released by the cancellation that
	// scope closure performs first, not by the shutdown that follows it. What
	// shutdown buys is for anything still holding the queue afterwards: it
	// learns the queue is finished instead of blocking on it forever.
	var escaped effect.Queue[int]

	program := effect.Scoped(func(scope effect.Scope) queueProgram[string] {
		return queueOperations.ScopedQueue[int](scope, 4, effect.SuspendWhenFull).
			Map(func(queue effect.Queue[int]) string {
				escaped = queue
				return "opened"
			})
	})
	if exit := effect.Run(context.Background(), effect.Unit{}, program); exit.IsFailure() {
		t.Fatalf("unexpected failure: %v", exit)
	}

	if !escaped.IsShutdown() {
		t.Fatal("expected scope closure to shut the queue down")
	}
	exit := effect.Run(context.Background(), effect.Unit{}, queueOperations.Take(escaped))
	received, ok := exit.Value()
	if !ok {
		t.Fatalf("expected a finished signal rather than a failure: %v", exit)
	}
	if received.OK {
		t.Fatalf("expected the queue to report that it is finished, got %#v", received)
	}
}

func TestAnUnscopedQueueOutlivesTheScopeThatFilledIt(t *testing.T) {
	// The contrast that makes the scoped form worth choosing: an unscoped queue
	// keeps its backlog and stays open, so its owner must shut it down.
	var escaped effect.Queue[int]

	program := effect.Scoped(func(effect.Scope) queueProgram[[]bool] {
		return effect.WidenError[string](effect.NewQueue[effect.Unit, int](4, effect.SuspendWhenFull)).
			FlatMap(func(queue effect.Queue[int]) queueProgram[[]bool] {
				escaped = queue
				return offerAll(queue, 5, 6)
			})
	})
	if exit := effect.Run(context.Background(), effect.Unit{}, program); exit.IsFailure() {
		t.Fatalf("unexpected failure: %v", exit)
	}

	if escaped.IsShutdown() {
		t.Fatal("expected an unscoped queue to stay open")
	}
	exit := effect.Run(context.Background(), effect.Unit{}, queueOperations.Take(escaped))
	if received, ok := exit.Value(); !ok || !received.OK || received.Value != 5 {
		t.Fatalf("expected the backlog to survive, got %v", exit)
	}
}

func TestQueueWaitsAreInterruptible(t *testing.T) {
	stop := errors.New("consumer gone")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(stop)

	program := queueOperations.Queue[int](1, effect.SuspendWhenFull).FlatMap(
		func(queue effect.Queue[int]) queueProgram[effect.Receive[int]] {
			return queueOperations.Take(queue)
		},
	)

	exit := effect.Run(ctx, effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected interruption, got %v", exit)
	}
	if interruption, ok := cause.Interruption(); !ok || !errors.Is(interruption.Cause, stop) {
		t.Fatalf("expected the caller's cancellation cause, got %v", cause)
	}
}

func TestConcurrentProducersAndConsumersLoseNoValues(t *testing.T) {
	const (
		producers = 8
		perEach   = 64
		total     = producers * perEach
	)
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())

	queued := effect.Run(context.Background(), effect.Unit{},
		effect.NewQueue[effect.Unit, int](16, effect.SuspendWhenFull))
	queue, ok := queued.Value()
	if !ok {
		t.Fatalf("could not create the queue: %v", queued)
	}

	var work sync.WaitGroup
	for producer := range producers {
		work.Go(func() {
			for index := range perEach {
				runtime.Run(context.Background(), effect.Unit{},
					queueOperations.Offer(queue, producer*perEach+index))
			}
		})
	}

	var consumed sync.Map
	var consumers sync.WaitGroup
	for range producers {
		consumers.Go(func() {
			for {
				exit := runtime.Run(context.Background(), effect.Unit{}, queueOperations.Take(queue))
				received, taken := exit.Value()
				if !taken || !received.OK {
					return
				}
				consumed.Store(received.Value, true)
			}
		})
	}

	work.Wait()
	effect.Run(context.Background(), effect.Unit{}, queue.Shutdown[effect.Unit]())
	consumers.Wait()

	seen := 0
	consumed.Range(func(any, any) bool {
		seen++
		return true
	})
	if seen != total {
		t.Fatalf("expected every value delivered exactly once, saw %d of %d", seen, total)
	}
}
