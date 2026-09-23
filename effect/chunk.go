package effect

import "slices"

// Chunk is a batch of values a stream moves as one unit.
//
// Streams are chunked rather than per-value because per-value plumbing costs an
// interpretation per element, and because adding chunks afterwards would change
// the type of every transform and sink.
//
// It is a value type rather than a bare slice for two reasons: a source cannot
// alias a buffer it later reuses, and the representation can change without
// touching callers.
type Chunk[A any] struct {
	values []A
}

// ChunkOf builds a chunk from values, copying them, so a caller that reuses its
// buffer cannot corrupt a chunk already emitted.
func ChunkOf[A any](values ...A) Chunk[A] {
	return Chunk[A]{values: slices.Clone(values)}
}

// Len reports how many values the chunk holds. An empty chunk is legal in the
// middle of a stream: a filter that rejected everything it was given produces
// one, and that is not the end of the stream.
func (chunk Chunk[A]) Len() int {
	return len(chunk.values)
}

// IsEmpty reports whether the chunk holds no values.
func (chunk Chunk[A]) IsEmpty() bool {
	return len(chunk.values) == 0
}

// Values returns the chunk's values. Treat the result as read-only; a chunk is
// shared with whoever it was emitted to.
func (chunk Chunk[A]) Values() []A {
	return chunk.values
}

// At returns the value at index, which must be within the chunk.
func (chunk Chunk[A]) At(index int) A {
	return chunk.values[index]
}

// MapChunk transforms every value. It is a package function because its result
// is built from the chunk's own type argument (golang/go#80172).
func MapChunk[A, B any](chunk Chunk[A], transform func(A) B) Chunk[B] {
	mapped := make([]B, 0, len(chunk.values))
	for _, value := range chunk.values {
		mapped = append(mapped, transform(value))
	}
	return Chunk[B]{values: mapped}
}

// FoldChunk accumulates over the chunk's values in order. It is a package
// function for the same reason as MapChunk.
func FoldChunk[A, S any](chunk Chunk[A], state S, combine func(S, A) S) S {
	for _, value := range chunk.values {
		state = combine(state, value)
	}
	return state
}

// Filter keeps the values predicate accepts.
func (chunk Chunk[A]) Filter(keep func(A) bool) Chunk[A] {
	kept := make([]A, 0, len(chunk.values))
	for _, value := range chunk.values {
		if keep(value) {
			kept = append(kept, value)
		}
	}
	return Chunk[A]{values: kept}
}

// TakeFirst returns at most count values from the front.
func (chunk Chunk[A]) TakeFirst(count int) Chunk[A] {
	if count >= len(chunk.values) {
		return chunk
	}
	if count < 1 {
		return Chunk[A]{}
	}
	return Chunk[A]{values: chunk.values[:count]}
}

// DropFirst discards at most count values from the front.
func (chunk Chunk[A]) DropFirst(count int) Chunk[A] {
	if count >= len(chunk.values) {
		return Chunk[A]{}
	}
	if count < 1 {
		return chunk
	}
	return Chunk[A]{values: chunk.values[count:]}
}

// TakeWhile returns the leading values predicate accepts, and reports whether
// it rejected one, which tells a stream that it has reached its end.
func (chunk Chunk[A]) TakeWhile(keep func(A) bool) (Chunk[A], bool) {
	for index, value := range chunk.values {
		if !keep(value) {
			return Chunk[A]{values: chunk.values[:index]}, true
		}
	}
	return chunk, false
}
