package unit

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang/effect"
)

type hubProgram[A any] = effect.Effect[effect.Unit, string, A]

var hubOperations = effect.For[effect.Unit, string]()

// drain takes the backlog a subscription is holding without blocking, which is
// what these cases want: they assert what was delivered, not that a stream
// eventually ended.
func drain(subscription effect.Subscription[int]) hubProgram[[]int] {
	return hubOperations.ReceiveAvailable(subscription, 64)
}

func TestHubDeliversEveryValueToEverySubscriber(t *testing.T) {
	// A channel gives each value to exactly one receiver. This is the case a
	// channel structurally cannot serve.
	program := effect.Scoped(func(scope effect.Scope) hubProgram[effect.Product[[]int, []int]] {
		return hubOperations.Hub[int](scope, 8, effect.SuspendWhenFull).FlatMap(
			func(hub effect.Hub[int]) hubProgram[effect.Product[[]int, []int]] {
				return effect.Zip(
					hubOperations.Subscribe(scope, hub),
					hubOperations.Subscribe(scope, hub),
				).FlatMap(func(both effect.Product[effect.Subscription[int], effect.Subscription[int]]) hubProgram[effect.Product[[]int, []int]] {
					return effect.ForEach([]int{1, 2, 3}, func(value int) hubProgram[bool] {
						return hubOperations.Publish(hub, value)
					}).AndThen(effect.Zip(drain(both.First), drain(both.Second)))
				})
			},
		)
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	received, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected failure: %v", exit)
	}
	want := []int{1, 2, 3}
	if !reflect.DeepEqual(received.First, want) || !reflect.DeepEqual(received.Second, want) {
		t.Fatalf("expected both subscribers to see %v, got %v and %v", want, received.First, received.Second)
	}
}

func TestHubDoesNotReplayValuesPublishedBeforeSubscribing(t *testing.T) {
	// Replay would make the hub's memory a function of how long the program has
	// run, so a late subscriber deliberately starts from now.
	program := effect.Scoped(func(scope effect.Scope) hubProgram[[]int] {
		return hubOperations.Hub[int](scope, 8, effect.SuspendWhenFull).FlatMap(
			func(hub effect.Hub[int]) hubProgram[[]int] {
				return hubOperations.Publish(hub, 1).
					AndThen(hubOperations.Subscribe(scope, hub)).
					FlatMap(func(late effect.Subscription[int]) hubProgram[[]int] {
						return hubOperations.Publish(hub, 2).AndThen(drain(late))
					})
			},
		)
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if got, ok := exit.Value(); !ok || !reflect.DeepEqual(got, []int{2}) {
		t.Fatalf("expected only what was published after subscribing, got %v", exit)
	}
}

func TestLeavingAScopeUnsubscribes(t *testing.T) {
	// A subscription is a resource because forgetting one is a leak: the hub
	// would keep filling an inbox nobody reads.
	var hub effect.Hub[int]

	program := effect.Scoped(func(outer effect.Scope) hubProgram[int] {
		return hubOperations.Hub[int](outer, 8, effect.SuspendWhenFull).FlatMap(
			func(created effect.Hub[int]) hubProgram[int] {
				hub = created
				inner := effect.Scoped(func(scope effect.Scope) hubProgram[int] {
					return hubOperations.Subscribe(scope, created).
						Map(func(effect.Subscription[int]) int { return created.Subscribers() })
				})
				return inner
			},
		)
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if inside, ok := exit.Value(); !ok || inside != 1 {
		t.Fatalf("expected one subscriber inside the scope, got %v", exit)
	}
	if remaining := hub.Subscribers(); remaining != 0 {
		t.Fatalf("expected leaving the scope to unsubscribe, %d remain", remaining)
	}
}

func TestAnEndedSubscriptionDrainsThenReportsFinished(t *testing.T) {
	var ended effect.Subscription[int]

	program := effect.Scoped(func(outer effect.Scope) hubProgram[string] {
		return hubOperations.Hub[int](outer, 8, effect.SuspendWhenFull).FlatMap(
			func(hub effect.Hub[int]) hubProgram[string] {
				return effect.Scoped(func(scope effect.Scope) hubProgram[string] {
					return hubOperations.Subscribe(scope, hub).FlatMap(
						func(subscription effect.Subscription[int]) hubProgram[string] {
							ended = subscription
							return hubOperations.Publish(hub, 9).As("published")
						},
					)
				})
			},
		)
	})
	if exit := effect.Run(context.Background(), effect.Unit{}, program); exit.IsFailure() {
		t.Fatalf("unexpected failure: %v", exit)
	}

	// The value published before the subscription ended is still there, and the
	// finished signal follows it.
	first := effect.Run(context.Background(), effect.Unit{}, hubOperations.Receive(ended))
	if received, ok := first.Value(); !ok || !received.OK || received.Value != 9 {
		t.Fatalf("expected the backlog to survive, got %v", first)
	}
	// Bounded on purpose. An inbox that unsubscribing failed to shut down would
	// block here forever, and a test that hangs reports nothing useful.
	second := effect.Run(context.Background(), effect.Unit{},
		effect.Timeout(hubOperations.Receive(ended), 2*time.Second))
	timed, ok := second.Value()
	if !ok {
		t.Fatalf("unexpected failure: %v", second)
	}
	received, completed := timed.RightValue()
	if !completed {
		t.Fatal("expected an ended subscription to report promptly, not block")
	}
	if received.OK {
		t.Fatalf("expected a finished signal, got %#v", received)
	}
}

func TestHubShutdownReleasesEverySubscriber(t *testing.T) {
	program := effect.Scoped(func(scope effect.Scope) hubProgram[effect.Product[[]int, bool]] {
		return hubOperations.Hub[int](scope, 8, effect.SuspendWhenFull).FlatMap(
			func(hub effect.Hub[int]) hubProgram[effect.Product[[]int, bool]] {
				return hubOperations.Subscribe(scope, hub).FlatMap(
					func(subscription effect.Subscription[int]) hubProgram[effect.Product[[]int, bool]] {
						return hubOperations.Publish(hub, 4).
							AndThen(hubOperations.WidenError(hub.Shutdown[effect.Unit]())).
							AndThen(effect.Zip(
								drain(subscription),
								hubOperations.Publish(hub, 5),
							))
					},
				)
			},
		)
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	result, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected failure: %v", exit)
	}
	if !reflect.DeepEqual(result.First, []int{4}) {
		t.Fatalf("expected the backlog delivered, got %v", result.First)
	}
	if result.Second {
		t.Fatal("expected a shut-down hub to refuse a publish")
	}
}

func TestSubscribingToAShutDownHubReportsThatItEnded(t *testing.T) {
	program := effect.Scoped(func(scope effect.Scope) hubProgram[effect.Subscription[int]] {
		return hubOperations.Hub[int](scope, 4, effect.SuspendWhenFull).FlatMap(
			func(hub effect.Hub[int]) hubProgram[effect.Subscription[int]] {
				return hubOperations.WidenError(hub.Shutdown[effect.Unit]()).
					AndThen(hubOperations.Subscribe(scope, hub))
			},
		)
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected subscribing to fail, got %v", exit)
	}
	interruption, ok := cause.Interruption()
	if !ok || !errors.Is(interruption.Cause, effect.ErrHubShutdown) {
		t.Fatalf("expected ErrHubShutdown, got %v", cause)
	}
}

func TestDroppingHubKeepsASlowSubscriberFromHoldingUpTheOthers(t *testing.T) {
	program := effect.Scoped(func(scope effect.Scope) hubProgram[effect.Product[[]int, []int]] {
		return hubOperations.Hub[int](scope, 2, effect.DropNewestWhenFull).FlatMap(
			func(hub effect.Hub[int]) hubProgram[effect.Product[[]int, []int]] {
				return effect.Zip(
					hubOperations.Subscribe(scope, hub),
					hubOperations.Subscribe(scope, hub),
				).FlatMap(func(both effect.Product[effect.Subscription[int], effect.Subscription[int]]) hubProgram[effect.Product[[]int, []int]] {
					// Four values into inboxes of two: publishing must not block.
					return effect.ForEach([]int{1, 2, 3, 4}, func(value int) hubProgram[bool] {
						return hubOperations.Publish(hub, value)
					}).AndThen(effect.Zip(drain(both.First), drain(both.Second)))
				})
			},
		)
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	received, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected failure: %v", exit)
	}
	want := []int{1, 2}
	if !reflect.DeepEqual(received.First, want) || !reflect.DeepEqual(received.Second, want) {
		t.Fatalf("expected each inbox to keep its first two, got %v and %v", received.First, received.Second)
	}
}
