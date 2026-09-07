package unit

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
)

var streamOperations = effect.For[effect.Unit, string]()

func collect(t *testing.T, stream effect.Stream[effect.Unit, string, int]) []int {
	t.Helper()
	exit := effect.Run(context.Background(), effect.Unit{}, effect.RunCollect(stream))
	values, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected failure: %v", exit)
	}
	return values
}

func TestStreamProducesItsValuesInOrder(t *testing.T) {
	if got := collect(t, streamOperations.StreamOf(1, 2, 3)); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("unexpected values: %v", got)
	}
}

func TestEmptyStreamProducesNothing(t *testing.T) {
	if got := collect(t, streamOperations.EmptyStream[int]()); len(got) != 0 {
		t.Fatalf("expected nothing, got %v", got)
	}
}

func TestStreamKeepsChunkBoundariesAndAnEmptyChunkIsNotTheEnd(t *testing.T) {
	// A filter that rejects everything in a chunk produces an empty chunk, and
	// that must not be read as the end of the stream.
	stream := streamOperations.StreamFromChunks(
		effect.ChunkOf(1, 2),
		effect.ChunkOf(3, 4),
		effect.ChunkOf(5),
	).FilterStream(func(value int) bool { return value > 4 })

	if got := collect(t, stream); !reflect.DeepEqual(got, []int{5}) {
		t.Fatalf("expected the later chunks still reached, got %v", got)
	}
}

func TestStreamTransformsCompose(t *testing.T) {
	doubled := effect.MapStream(
		streamOperations.StreamOf(1, 2, 3, 4, 5),
		func(value int) int { return value * 2 },
	)
	stream := doubled.FilterStream(func(value int) bool { return value%4 == 0 }).DropStream(1)

	if got := collect(t, stream); !reflect.DeepEqual(got, []int{8}) {
		t.Fatalf("unexpected values: %v", got)
	}
}

func TestMapStreamChangesTheElementType(t *testing.T) {
	stream := effect.MapStream(
		streamOperations.StreamOf(1, 2, 3),
		func(value int) string { return string(rune('a' + value - 1)) },
	)

	exit := effect.Run(context.Background(), effect.Unit{}, effect.RunCollect(stream))
	if got, ok := exit.Value(); !ok || !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("unexpected values: %v", exit)
	}
}

func TestTakeStreamDoesNotPullAValueNobodyWillUse(t *testing.T) {
	// The guarantee that makes Take usable on a blocking or expensive source:
	// taking three values must not evaluate a fourth.
	var evaluations atomic.Int32
	counting := streamOperations.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		return effect.ExitSuccess[string](int(evaluations.Add(1)))
	})

	stream := effect.StreamRepeatEffect(counting).TakeStream(3)
	if got := collect(t, stream); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("unexpected values: %v", got)
	}
	if evaluated := evaluations.Load(); evaluated != 3 {
		t.Fatalf("expected exactly three evaluations, got %d", evaluated)
	}
}

func TestTakeStreamWhileEndsAtTheFirstRejection(t *testing.T) {
	stream := streamOperations.StreamOf(1, 2, 3, 1, 2).
		TakeStreamWhile(func(value int) bool { return value < 3 })

	if got := collect(t, stream); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("expected the stream to end at the rejection, got %v", got)
	}
}

func TestStreamFromEffectProducesExactlyOneValue(t *testing.T) {
	var evaluations atomic.Int32
	once := streamOperations.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		return effect.ExitSuccess[string](int(evaluations.Add(1)))
	})

	if got := collect(t, effect.StreamFromEffect(once)); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("unexpected values: %v", got)
	}
	if evaluated := evaluations.Load(); evaluated != 1 {
		t.Fatalf("expected exactly one evaluation, got %d", evaluated)
	}
}

func TestConcatenatedStreamsProduceBothInOrder(t *testing.T) {
	stream := effect.ConcatStreams(
		streamOperations.StreamOf(1, 2),
		streamOperations.StreamOf(3, 4),
	)

	if got := collect(t, stream); !reflect.DeepEqual(got, []int{1, 2, 3, 4}) {
		t.Fatalf("unexpected values: %v", got)
	}
}

func TestStreamFailurePropagatesToTheSink(t *testing.T) {
	stream := effect.ConcatStreams(
		streamOperations.StreamOf(1),
		effect.StreamFail[effect.Unit, int]("source unavailable"),
	)

	exit := effect.Run(context.Background(), effect.Unit{}, effect.RunCollect(stream))
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected the failure to reach the sink, got %v", exit)
	}
	if failure, ok := cause.Failure(); !ok || failure != "source unavailable" {
		t.Fatalf("unexpected cause: %v", cause)
	}
}

func TestSinksAgreeOnWhatTheStreamProduced(t *testing.T) {
	stream := streamOperations.StreamOf(1, 2, 3, 4)

	counted := effect.Run(context.Background(), effect.Unit{}, effect.RunCount(stream))
	if count, ok := counted.Value(); !ok || count != 4 {
		t.Fatalf("unexpected count: %v", counted)
	}
	summed := effect.Run(context.Background(), effect.Unit{},
		effect.RunFold(stream, 0, func(total int, value int) int { return total + value }))
	if total, ok := summed.Value(); !ok || total != 10 {
		t.Fatalf("unexpected total: %v", summed)
	}
	if drained := effect.Run(context.Background(), effect.Unit{}, effect.RunDrain(stream)); drained.IsFailure() {
		t.Fatalf("unexpected drain failure: %v", drained)
	}
}

func TestOneStreamValueRunsRepeatedlyWithoutSharingState(t *testing.T) {
	// The invariant that makes a Stream reusable: per-run state is built when
	// the stream is opened, not when it is described.
	stream := streamOperations.StreamOf(1, 2, 3).TakeStream(2)

	for attempt := range 3 {
		if got := collect(t, stream); !reflect.DeepEqual(got, []int{1, 2}) {
			t.Fatalf("run %d saw %v", attempt, got)
		}
	}
}

func TestDeepStreamConsumptionAddsNoStackFrames(t *testing.T) {
	const values = 200_000
	var produced atomic.Int32
	counting := streamOperations.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		return effect.ExitSuccess[string](int(produced.Add(1)))
	})

	exit := effect.Run(context.Background(), effect.Unit{},
		effect.RunCount(effect.StreamRepeatEffect(counting).TakeStream(values)))
	if count, ok := exit.Value(); !ok || count != values {
		t.Fatalf("unexpected count: %v", exit)
	}
}
