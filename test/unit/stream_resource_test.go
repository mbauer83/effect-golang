package unit

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effecttest"
)

// A stream's sources are acquired in the consumer's scope, so what a source
// holds is released when the consumer is finished with it -- including when the
// consumer stopped early, failed, or was cancelled.

// trackSource is a stream whose source acquires a resource and records each
// value as it is read, so a test can see the release relative to the reads
// rather than merely that both happened.
func trackSource(tracker *effecttest.Tracker, values ...int) effect.Stream[effect.Unit, string, int] {
	return effect.StreamFromResource(
		func(scope effect.Scope) effect.Effect[effect.Unit, string, []int] {
			return effecttest.TrackResource[effect.Unit, string](scope, tracker, "source").
				As(values)
		},
		func(held []int) effect.Stream[effect.Unit, string, int] {
			return effect.MapStream(streamOperations.StreamOf(held...), func(value int) int {
				tracker.Record("read " + strconv.Itoa(value))
				return value
			})
		},
	)
}

func TestAStreamReleasesItsSourceWhenTheConsumerIsDone(t *testing.T) {
	tracker := &effecttest.Tracker{}

	if got := collect(t, trackSource(tracker, 1, 2, 3)); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("unexpected values: %v", got)
	}
	// The release must come after the reads. Asserting only that both happened
	// would pass an implementation that released the source before using it.
	want := []string{"acquire source", "read 1", "read 2", "read 3", "release source"}
	if got := tracker.Events(); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestAStreamReleasesItsSourceWhenTheConsumerStopsEarly(t *testing.T) {
	tracker := &effecttest.Tracker{}
	stream := trackSource(tracker, 1, 2, 3, 4, 5).TakeStream(2)

	if got := collect(t, stream); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("unexpected values: %v", got)
	}
	if released := tracker.Count("release source"); released != 1 {
		t.Fatalf("expected the source released exactly once, got %d", released)
	}
}

func TestAStreamReleasesItsSourceWhenTheConsumerFails(t *testing.T) {
	tracker := &effecttest.Tracker{}
	stream := trackSource(tracker, 1, 2, 3)

	exit := effect.Run(context.Background(), effect.Unit{},
		effect.RunForEach(stream, func(value int) effect.Effect[effect.Unit, string, effect.Unit] {
			if value == 2 {
				return streamOperations.Fail[effect.Unit]("rejected")
			}
			return streamOperations.Succeed(effect.Unit{})
		}),
	)
	if exit.IsSuccess() {
		t.Fatalf("expected the visitor's failure, got %v", exit)
	}
	if released := tracker.Count("release source"); released != 1 {
		t.Fatalf("expected the source released after a failure, got %d", released)
	}
}

func TestAStreamReleasesItsSourceWhenTheCallerCancels(t *testing.T) {
	tracker := &effecttest.Tracker{}
	stop := errors.New("caller stopped reading")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(stop)

	exit := effect.Run(ctx, effect.Unit{}, effect.RunCollect(trackSource(tracker, 1, 2)))
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected interruption, got %v", exit)
	}
	if interruption, ok := cause.Interruption(); !ok || !errors.Is(interruption.Cause, stop) {
		t.Fatalf("expected the caller's cancellation cause, got %v", cause)
	}
	// The source was never acquired, so there is nothing to release; what must
	// not happen is an acquisition left unreleased.
	if acquired, released := tracker.Count("acquire source"), tracker.Count("release source"); acquired != released {
		t.Fatalf("expected acquisitions and releases to match, got %d and %d", acquired, released)
	}
}

func TestConcatenatedStreamsReleaseBothSources(t *testing.T) {
	tracker := &effecttest.Tracker{}
	stream := effect.ConcatStreams(
		trackSource(tracker, 1),
		trackSource(tracker, 2),
	)

	if got := collect(t, stream); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("unexpected values: %v", got)
	}
	if released := tracker.Count("release source"); released != 2 {
		t.Fatalf("expected both sources released, got %d", released)
	}
}

func TestAStreamBridgesIntoAQueueAndShutsItDown(t *testing.T) {
	// A stream pushed into a queue lets work pull at its own pace, and the
	// shutdown is why a consumer of that queue learns the stream ended.
	program := effect.Scoped(func(scope effect.Scope) effect.Effect[effect.Unit, string, []int] {
		return streamOperations.ScopedQueue[int](scope, 8, effect.SuspendWhenFull).FlatMap(
			func(queue effect.Queue[int]) effect.Effect[effect.Unit, string, []int] {
				return effect.RunIntoQueue(streamOperations.StreamOf(1, 2, 3), queue).
					AndThen(effect.RunCollect(streamOperations.StreamFromQueue(queue, 4)))
			},
		)
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if got, ok := exit.Value(); !ok || !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("unexpected values: %v", exit)
	}
}

func TestAStreamBridgesIntoAHubAndEveryFeedSeesIt(t *testing.T) {
	program := effect.Scoped(func(scope effect.Scope) effect.Effect[effect.Unit, string, effect.Product[[]int, []int]] {
		return streamOperations.Hub[int](scope, 8, effect.SuspendWhenFull).FlatMap(
			func(hub effect.Hub[int]) effect.Effect[effect.Unit, string, effect.Product[[]int, []int]] {
				return effect.Zip(
					streamOperations.Subscribe(scope, hub),
					streamOperations.Subscribe(scope, hub),
				).FlatMap(func(both effect.Product[effect.Subscription[int], effect.Subscription[int]]) effect.Effect[effect.Unit, string, effect.Product[[]int, []int]] {
					return effect.RunIntoHub(streamOperations.StreamOf(7, 8), hub).
						AndThen(effect.Zip(
							effect.RunCollect(streamOperations.StreamFromSubscription(both.First, 4)),
							effect.RunCollect(streamOperations.StreamFromSubscription(both.Second, 4)),
						))
				})
			},
		)
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	received, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected failure: %v", exit)
	}
	want := []int{7, 8}
	if !reflect.DeepEqual(received.First, want) || !reflect.DeepEqual(received.Second, want) {
		t.Fatalf("expected both feeds to see %v, got %v and %v", want, received.First, received.Second)
	}
}
