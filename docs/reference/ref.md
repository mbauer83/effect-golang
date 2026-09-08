# Ref reference

A mutable cell whose operations are effects, one at a time.

```go
program := effect.NewRef[effect.Unit](0).
    FlatMap(func(counter effect.Ref[int]) effect.Effect[effect.Unit, effect.Never, int] {
        return counter.Update[effect.Unit](func(current int) int { return current + 1 }).
            FlatMap(func(effect.Unit) effect.Effect[effect.Unit, effect.Never, int] {
                return counter.Get[effect.Unit]()
            })
    })
```

It is the piece between a plain variable and a `Queue`. A queue hands values
from one fiber to another and a `Deferred` broadcasts one result; neither is the
shape for state that is simply read and written, and a program needing that was
writing a struct with a mutex and wrapping the calls by hand.

## Creation is an effect

`NewRef` returns an effect, as `NewDeferred` does, and for the same reason:
state that existed before interpretation would be shared between runs and
between the attempts of a retry, which is never what the program meant. A `Ref`
value cannot exist until something interprets the description that makes it.

## Operations

```go
func NewRef[R, A any](initial A) Effect[R, Never, Ref[A]]

func (ref Ref[A]) Get[R any]() Effect[R, Never, A]
func (ref Ref[A]) Set[R any](value A) Effect[R, Never, Unit]
func (ref Ref[A]) Update[R any](change func(A) A) Effect[R, Never, Unit]
func (ref Ref[A]) GetAndSet[R any](value A) Effect[R, Never, A]
func (ref Ref[A]) GetAndUpdate[R any](change func(A) A) Effect[R, Never, A]
func (ref Ref[A]) UpdateAndGet[R any](change func(A) A) Effect[R, Never, A]

func Modify[R, A, B any](ref Ref[A], change func(A) (A, B)) Effect[R, Never, B]
```

None of them can fail, so the failure channel is `Never`.

`Modify` is the one the others are written in terms of, because it is the only
one that can **decide and write in the same step**. A check followed by an
update is two operations and two fibers can interleave between them:

```go
added := effect.Modify(books, func(held []Book) ([]Book, bool) {
    if slices.ContainsFunc(held, sameTitle(book)) {
        return held, false
    }
    return append(held, book), true
})
```

`Modify` is a package function because a method cannot introduce the result
type it derives.

## What is guaranteed, and what is not

**One operation on one Ref is atomic.** A thousand parallel `Update`s leave a
thousand increments; removing the lock loses some, and the race detector says
so.

**A change is applied exactly once.** A lock-free cell would retry on
contention and so apply the change more than once — safe only for a change that
happens to be pure, and nothing in Go can promise one is. The cost is a mutex
held for the duration of the change, so a change should be short and must not
block.

**Two operations are not atomic, and neither are two Refs.** `Get` then `Set` is
two steps; so is `Update` on one Ref and `Update` on another. Where several
values have to move together, put them in **one Ref as one value** and change it
with `Modify`. That is the answer in almost every case, and the reason this is
not the beginning of an STM: composing atomicity across independently created
cells is what STM is for, it needs its own retry machinery, and its correctness
rests on transaction bodies being pure and replayable — which Scala's `ZSTM`
enforces structurally and Go cannot.

## Where it fits

| For | Reach for |
|---|---|
| state read and written by several fibers | `Ref` |
| handing values from one fiber to another | a channel, then `Queue` |
| one result many waiters must see | `Deferred` |
| broadcasting to every subscriber | `Hub` |
| a value that must be released | `Scope.AcquireRelease` |
