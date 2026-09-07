package unit

import (
	"reflect"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
)

func TestChunkOfCopiesSoASourceCannotCorruptWhatItEmitted(t *testing.T) {
	// The reason Chunk is a type rather than a bare slice: a source that reuses
	// its buffer must not be able to change a chunk it has already handed over.
	buffer := []int{1, 2, 3}
	chunk := effect.ChunkOf(buffer...)
	buffer[0] = 99

	if got := chunk.Values(); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("expected the chunk to be unaffected, got %v", got)
	}
}

func TestChunkReportsItsContents(t *testing.T) {
	chunk := effect.ChunkOf("a", "b", "c")

	if chunk.Len() != 3 || chunk.IsEmpty() {
		t.Fatalf("unexpected size: len=%d empty=%v", chunk.Len(), chunk.IsEmpty())
	}
	if chunk.At(1) != "b" {
		t.Fatalf("unexpected value at 1: %q", chunk.At(1))
	}

	var zero effect.Chunk[string]
	if zero.Len() != 0 || !zero.IsEmpty() || len(zero.Values()) != 0 {
		t.Fatalf("expected the zero chunk to be empty, got %#v", zero)
	}
}

func TestChunkSlicingClampsRatherThanPanicking(t *testing.T) {
	chunk := effect.ChunkOf(1, 2, 3)
	cases := map[string]struct {
		got  effect.Chunk[int]
		want []int
	}{
		"take fewer":    {chunk.TakeFirst(2), []int{1, 2}},
		"take more":     {chunk.TakeFirst(9), []int{1, 2, 3}},
		"take none":     {chunk.TakeFirst(0), nil},
		"take negative": {chunk.TakeFirst(-1), nil},
		"drop fewer":    {chunk.DropFirst(1), []int{2, 3}},
		"drop more":     {chunk.DropFirst(9), nil},
		"drop none":     {chunk.DropFirst(0), []int{1, 2, 3}},
		"drop negative": {chunk.DropFirst(-1), []int{1, 2, 3}},
	}

	for name, check := range cases {
		got := check.got.Values()
		if len(got) == 0 && len(check.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, check.want) {
			t.Fatalf("%s: expected %v, got %v", name, check.want, got)
		}
	}
}

func TestChunkFilterAndTakeWhile(t *testing.T) {
	chunk := effect.ChunkOf(1, 2, 3, 4, 1)

	if got := chunk.Filter(func(value int) bool { return value%2 == 0 }).Values(); !reflect.DeepEqual(got, []int{2, 4}) {
		t.Fatalf("unexpected filtered values: %v", got)
	}

	kept, rejected := chunk.TakeWhile(func(value int) bool { return value < 3 })
	if !rejected || !reflect.DeepEqual(kept.Values(), []int{1, 2}) {
		t.Fatalf("expected a rejection after two values, got %v rejected=%v", kept.Values(), rejected)
	}

	all, none := chunk.TakeWhile(func(int) bool { return true })
	if none || all.Len() != chunk.Len() {
		t.Fatalf("expected no rejection, got %v rejected=%v", all.Values(), none)
	}
}

func TestChunkMapAndFold(t *testing.T) {
	chunk := effect.ChunkOf(1, 2, 3)

	doubled := effect.MapChunk(chunk, func(value int) string {
		return string(rune('0' + value*2))
	})
	if got := doubled.Values(); !reflect.DeepEqual(got, []string{"2", "4", "6"}) {
		t.Fatalf("unexpected mapped values: %v", got)
	}
	if total := effect.FoldChunk(chunk, 0, func(sum int, value int) int { return sum + value }); total != 6 {
		t.Fatalf("unexpected fold result: %d", total)
	}
}
