package unit

import (
	"context"
	"reflect"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
)

// The remaining sources, the structures a stream reads from, and the seam a
// caller uses to write a combinator this package does not provide.

func TestACustomSourceBuildsItsOwnSteps(t *testing.T) {
	// Emit and EndOfStream are the whole protocol a hand-written source needs.
	counting := func() effect.Stream[effect.Unit, string, int] {
		return effect.StreamFromResource(
			func(effect.Scope) effect.Effect[effect.Unit, string, *int] {
				return streamOperations.Suspend(func() effect.Effect[effect.Unit, string, *int] {
					remaining := 3
					return streamOperations.Succeed(&remaining)
				})
			},
			func(remaining *int) effect.Stream[effect.Unit, string, int] {
				return effect.StreamRepeatEffect(streamOperations.From(
					func(context.Context, effect.Unit) effect.Exit[string, int] {
						return effect.ExitSuccess[string](*remaining)
					},
				)).TakeStream(*remaining)
			},
		)
	}

	if got := collect(t, counting()); !reflect.DeepEqual(got, []int{3, 3, 3}) {
		t.Fatalf("unexpected values: %v", got)
	}
}

func TestStepReportsWhatItCarries(t *testing.T) {
	carried, more := effect.Emit(effect.ChunkOf(1, 2)).Chunk()
	if !more || !reflect.DeepEqual(carried.Values(), []int{1, 2}) {
		t.Fatalf("unexpected emitted step: %v more=%v", carried.Values(), more)
	}

	ended, more := effect.EndOfStream[int]().Chunk()
	if more || !ended.IsEmpty() {
		t.Fatalf("unexpected end step: %v more=%v", ended.Values(), more)
	}

	// The zero value is the end, so a source that forgets to say so cannot
	// produce an infinite stream of nothing.
	var zero effect.Step[int]
	if _, keepGoing := zero.Chunk(); keepGoing {
		t.Fatal("expected the zero Step to be the end of the stream")
	}
}

func TestUnboundedScopedQueueAndTakeAvailable(t *testing.T) {
	program := effect.Scoped(func(scope effect.Scope) effect.Effect[effect.Unit, string, []int] {
		return effect.WidenError[string](scope.UnboundedQueue[effect.Unit, int]()).FlatMap(
			func(queue effect.Queue[int]) effect.Effect[effect.Unit, string, []int] {
				// TakeAvailable never waits, so an empty result means empty now.
				empty := effect.Run(context.Background(), effect.Unit{},
					queueOperations.TakeAvailable(queue, 4))
				if drained, ok := empty.Value(); !ok || len(drained) != 0 {
					t.Errorf("expected an empty non-blocking take, got %v", empty)
				}
				return offerAll(queue, 1, 2, 3).AndThen(queueOperations.TakeAvailable(queue, 4))
			},
		)
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if got, ok := exit.Value(); !ok || !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("unexpected values: %v", exit)
	}
}

func TestAnUnscopedHubAndADeferredCompletedWithAWholeOutcome(t *testing.T) {
	program := effect.Scoped(func(scope effect.Scope) effect.Effect[effect.Unit, string, int] {
		return effect.WidenError[string](effect.NewHub[effect.Unit, int](4, effect.SuspendWhenFull)).
			FlatMap(func(hub effect.Hub[int]) effect.Effect[effect.Unit, string, int] {
				return hubOperations.Subscribe(scope, hub).FlatMap(
					func(feed effect.Subscription[int]) effect.Effect[effect.Unit, string, int] {
						return hubOperations.Publish(hub, 5).
							AndThen(hubOperations.Publish(hub, 6)).
							FlatMap(func(bool) effect.Effect[effect.Unit, string, int] {
								if pending := feed.Pending(); pending != 2 {
									t.Errorf("expected two pending values, got %d", pending)
								}
								return hubOperations.ReceiveUpTo(feed, 2).
									Map(func(batch []int) int { return len(batch) })
							})
					},
				)
			})
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if got, ok := exit.Value(); !ok || got != 2 {
		t.Fatalf("unexpected exit: %v", exit)
	}
}

func TestADeferredCompletedWithAnInterruptionHandsItToEveryWaiter(t *testing.T) {
	// Complete takes a whole Exit so a defect or an interruption reaches the
	// waiters rather than being lost on the way.
	program := effect.WidenError[string](effect.NewDeferred[effect.Unit, string, int]()).
		FlatMap(func(pending effect.Deferred[string, int]) effect.Effect[effect.Unit, string, int] {
			handed := effect.ExitCause[string, int](effect.InterruptCause[string](effect.ErrTimedOut))
			return effect.WidenError[string](pending.Complete[effect.Unit](handed)).
				AndThen(pending.Await[effect.Unit]())
		})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed || !cause.IsInterruptedOnly() {
		t.Fatalf("expected the handed interruption, got %v", exit)
	}
}

func TestTheInterpreterSeamLetsACallerWriteItsOwnCombinator(t *testing.T) {
	// What Interpreting is for: a combinator this package does not provide,
	// evaluating sub-effects in the current interpretation so they see its
	// capabilities, scope and cancellation.
	firstSucceeding := func(candidates ...effect.Effect[effect.Unit, string, int]) effect.Effect[effect.Unit, string, int] {
		return effect.Interpreting(func(interpreter effect.Interpreter[effect.Unit, string]) effect.Exit[string, int] {
			var last effect.Exit[string, int]
			for _, candidate := range candidates {
				last = effect.Evaluate(interpreter, candidate)
				if last.IsSuccess() {
					return last
				}
				if interpreter.Context().Err() != nil {
					return last
				}
			}
			return last
		})
	}

	exit := effect.Run(context.Background(), effect.Unit{}, firstSucceeding(
		streamOperations.Fail[int]("first unavailable"),
		streamOperations.Fail[int]("second unavailable"),
		streamOperations.Succeed(42),
	))
	if got, ok := exit.Value(); !ok || got != 42 {
		t.Fatalf("unexpected exit: %v", exit)
	}
}

func TestAHandWrittenSourceCanEnd(t *testing.T) {
	// Every other constructor either knows its values in advance or never
	// finishes, so a source that reads until something closes -- a socket, a
	// cursor -- needs this seam. Emit and EndOfStream are the whole protocol.
	reading := func(available []int) effect.Stream[effect.Unit, string, int] {
		return effect.StreamFromSteps(func() effect.Effect[effect.Unit, string, effect.Step[int]] {
			remaining := available
			return streamOperations.From(
				func(context.Context, effect.Unit) effect.Exit[string, effect.Step[int]] {
					if len(remaining) == 0 {
						return effect.ExitSuccess[string](effect.EndOfStream[int]())
					}
					next := remaining[0]
					remaining = remaining[1:]
					return effect.ExitSuccess[string](effect.Emit(effect.ChunkOf(next)))
				})
		})
	}

	stream := reading([]int{1, 2, 3})
	if got := collect(t, stream); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("unexpected values: %v", got)
	}
	// Per-run state, so one Stream value stays reusable: a second run reads the
	// same values rather than finding the source spent.
	if got := collect(t, stream); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("a second run saw %v", got)
	}
}
