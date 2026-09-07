package effect_test

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	effect "github.com/mbauer83/effect-golang"
)

func TestSendAndRecvMoveValuesThroughANativeChannel(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	pipe := make(chan int, 1)

	program := operations.Send(pipe, 7).
		AndThen(operations.Recv[int](pipe)).
		Map(func(received effect.Receive[int]) int {
			if !received.OK {
				t.Error("expected an open channel")
			}
			return received.Value
		})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if value, ok := exit.Value(); !ok || value != 7 {
		t.Fatalf("unexpected exit: %v", exit)
	}
}

func TestRecvOnClosedChannelIsNotAFailure(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	pipe := make(chan string)
	close(pipe)

	exit := effect.Run(context.Background(), effect.Unit{}, operations.Recv[string](pipe))
	received, ok := exit.Value()
	if !ok {
		t.Fatalf("expected closure to be reported as a value, got %v", exit)
	}
	if received.OK || received.Value != "" {
		t.Fatalf("expected the zero value and OK false, got %#v", received)
	}
}

func TestRecvOrFailTurnsClosureIntoATypedFailure(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	pipe := make(chan string)
	close(pipe)

	exit := effect.Run(context.Background(), effect.Unit{},
		operations.RecvOrFail(pipe, "producer finished"))

	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected a typed failure, got %v", exit)
	}
	if failure, ok := cause.Failure(); !ok || failure != "producer finished" {
		t.Fatalf("unexpected cause: %v", cause)
	}
}

func TestSendToClosedChannelIsADefect(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	pipe := make(chan int, 1)
	close(pipe)

	exit := effect.Run(context.Background(), effect.Unit{}, operations.Send(pipe, 1))
	cause, failed := exit.Cause()
	if !failed || !cause.ContainsDefect() {
		t.Fatalf("expected a defect for a send to a closed channel, got %v", exit)
	}
	if failures := cause.Failures(); len(failures) != 0 {
		t.Fatalf("expected no typed failure, got %#v", failures)
	}
}

func TestChannelWaitsAreInterruptibleAndLeaveNoGoroutine(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	stop := errors.New("consumer gone")
	before := runtime.NumGoroutine()

	cases := map[string]effect.Effect[effect.Unit, string, effect.Unit]{
		"blocked send":    operations.Send(make(chan int), 1),
		"blocked receive": operations.Recv[int](make(chan int)).As(effect.Unit{}),
		"nil send":        operations.Send(chan<- int(nil), 1),
		"nil receive":     operations.Recv[int](nil).As(effect.Unit{}),
	}

	for name, program := range cases {
		ctx, cancel := context.WithCancelCause(context.Background())
		go func() {
			time.Sleep(time.Millisecond)
			cancel(stop)
		}()

		exit := effect.Run(ctx, effect.Unit{}, program)
		cancel(stop)

		cause, failed := exit.Cause()
		if !failed {
			t.Fatalf("%s: expected interruption, got %v", name, exit)
		}
		interruption, ok := cause.Interruption()
		if !ok || !errors.Is(interruption.Cause, stop) {
			t.Fatalf("%s: expected the cancellation cause, got %v", name, cause)
		}
	}

	settleGoroutines()
	if after := runtime.NumGoroutine(); after > before+2 {
		t.Fatalf("expected no leaked goroutines, went from %d to %d", before, after)
	}
}

// settleGoroutines gives already-canceled goroutines a chance to finish before
// a leak count is taken.
func settleGoroutines() {
	for range 100 {
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
}

func TestFiberDoneInteroperatesWithAnOrdinarySelect(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	requests := make(chan int, 1)
	replies := make(chan int, 1)

	// A worker fiber serves one request, then finishes. The caller waits with a
	// plain select over the fiber's completion signal and its own channels.
	worker := operations.Recv[int](requests).FlatMap(
		func(received effect.Receive[int]) effect.Effect[effect.Unit, string, effect.Unit] {
			return operations.Send(replies, received.Value*3)
		},
	)

	program := effect.Scoped(func(effect.Scope) effect.Effect[effect.Unit, string, int] {
		return operations.Fork(worker).FlatMap(
			func(fiber effect.Fiber[string, effect.Unit]) effect.Effect[effect.Unit, string, int] {
				requests <- 14
				for {
					select {
					case reply := <-replies:
						return operations.Succeed(reply)
					case <-fiber.Done():
						if exit, _ := fiber.Poll(); exit.IsFailure() {
							return operations.Fail[int]("worker failed: " + exit.String())
						}
					}
				}
			},
		)
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if value, ok := exit.Value(); !ok || value != 42 {
		t.Fatalf("unexpected exit: %v", exit)
	}
}
