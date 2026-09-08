package unit

// The transforms: whole chunks, an effect per value, and an effect that decides
// how many values a value becomes.

import (
	"context"
	"reflect"
	"strconv"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effecttest"
)

func TestMapStreamChunksTransformsWholeBatches(t *testing.T) {
	stream := effect.MapStreamChunks(
		streamOperations.StreamFromChunks(effect.ChunkOf(1, 2), effect.ChunkOf(3)),
		func(chunk effect.Chunk[int]) effect.Chunk[string] {
			return effect.ChunkOf(strconv.Itoa(chunk.Len()))
		},
	)

	exit := effect.Run(context.Background(), effect.Unit{}, effect.RunCollect(stream))
	if got, ok := exit.Value(); !ok || !reflect.DeepEqual(got, []string{"2", "1"}) {
		t.Fatalf("expected one value per chunk, got %v", exit)
	}
}

func TestMapStreamEffectEvaluatesInOrderAndPropagatesFailure(t *testing.T) {
	tracker := &effecttest.Tracker{}
	visiting := func(value int) effect.Effect[effect.Unit, string, int] {
		return streamOperations.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
			tracker.Record(strconv.Itoa(value))
			if value == 3 {
				return effect.ExitFailure[string, int]("rejected")
			}
			return effect.ExitSuccess[string](value * 10)
		})
	}

	exit := effect.Run(context.Background(), effect.Unit{},
		effect.RunCollect(effect.MapStreamEffect(streamOperations.StreamOf(1, 2, 3, 4), visiting)))
	if exit.IsSuccess() {
		t.Fatalf("expected the transform's failure, got %v", exit)
	}
	if got := tracker.Events(); !reflect.DeepEqual(got, []string{"1", "2", "3"}) {
		t.Fatalf("expected evaluation in order, stopping at the failure, got %v", got)
	}
}

func TestCollectStreamEffectDropsExpandsAndReplaces(t *testing.T) {
	// The three things a one-for-one transform cannot do, decided by an effect
	// rather than by a predicate: a value with nowhere to go, a value that
	// stands for several, and a value that is simply transformed.
	tracker := &effecttest.Tracker{}
	sorting := func(value int) effect.Effect[effect.Unit, string, effect.Chunk[string]] {
		return streamOperations.From(
			func(context.Context, effect.Unit) effect.Exit[string, effect.Chunk[string]] {
				tracker.Record(strconv.Itoa(value))
				switch {
				case value < 0:
					return effect.ExitSuccess[string](effect.ChunkOf[string]())
				case value == 0:
					return effect.ExitSuccess[string](effect.ChunkOf("zero", "still zero"))
				}
				return effect.ExitSuccess[string](effect.ChunkOf(strconv.Itoa(value)))
			})
	}

	stream := effect.CollectStreamEffect(streamOperations.StreamOf(-1, 0, 2, -3), sorting)
	if got := collect(t, stream); !reflect.DeepEqual(got, []string{"zero", "still zero", "2"}) {
		t.Fatalf("unexpected values: %v", got)
	}
	// Every value was visited, including the ones that produced nothing: a
	// transform that decides is the thing being asked, so it has to be asked.
	if got := tracker.Events(); !reflect.DeepEqual(got, []string{"-1", "0", "2", "-3"}) {
		t.Fatalf("unexpected visits: %v", got)
	}
}

func TestCollectStreamEffectLosingEveryValueIsNotTheEndOfTheStream(t *testing.T) {
	// A chunk that loses everything must not look like the end, or a stream
	// would terminate at the first batch nothing survived.
	dropping := func(int) effect.Effect[effect.Unit, string, effect.Chunk[int]] {
		return streamOperations.Succeed(effect.ChunkOf[int]())
	}

	stream := effect.CollectStreamEffect(
		streamOperations.StreamFromChunks(effect.ChunkOf(1, 2), effect.ChunkOf(3)), dropping)
	if got := collect(t, stream); len(got) != 0 {
		t.Fatalf("expected nothing to survive, got %v", got)
	}

	// And the stream that follows an empty chunk still runs: the second chunk's
	// values arrive when the transform keeps them.
	kept := 0
	counting := func(value int) effect.Effect[effect.Unit, string, effect.Chunk[int]] {
		return streamOperations.Suspend(func() effect.Effect[effect.Unit, string, effect.Chunk[int]] {
			kept++
			if kept < 3 {
				return streamOperations.Succeed(effect.ChunkOf[int]())
			}
			return streamOperations.Succeed(effect.ChunkOf(value))
		})
	}
	surviving := effect.CollectStreamEffect(
		streamOperations.StreamFromChunks(effect.ChunkOf(1, 2), effect.ChunkOf(3)), counting)
	if got := collect(t, surviving); !reflect.DeepEqual(got, []int{3}) {
		t.Fatalf("expected the third value only, got %v", got)
	}
}
