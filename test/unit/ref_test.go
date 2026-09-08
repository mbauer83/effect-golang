package unit

// Ref: a mutable cell whose operations are effects. What matters is that one
// operation is atomic, that a change is applied exactly once, and that a cell
// belongs to one interpretation rather than to the description that created it.

import (
	"context"
	"reflect"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
)

// ran interprets a program over no environment.
func ran[A any](t *testing.T, program effect.Effect[effect.Unit, effect.Never, A]) A {
	t.Helper()
	value, succeeded := effect.Run(context.Background(), effect.Unit{}, program).Value()
	if !succeeded {
		t.Fatal("expected a program that cannot fail to succeed")
	}
	return value
}

func TestARefHoldsWhatItWasGivenAndWhatItIsSet(t *testing.T) {
	program := effect.NewRef[effect.Unit](7).
		FlatMap(func(ref effect.Ref[int]) effect.Effect[effect.Unit, effect.Never, []int] {
			return ref.Get[effect.Unit]().
				FlatMap(func(first int) effect.Effect[effect.Unit, effect.Never, []int] {
					return ref.Set[effect.Unit](9).
						FlatMap(func(effect.Unit) effect.Effect[effect.Unit, effect.Never, []int] {
							return ref.Get[effect.Unit]().Map(func(second int) []int {
								return []int{first, second}
							})
						})
				})
		})

	if got := ran(t, program); !reflect.DeepEqual(got, []int{7, 9}) {
		t.Fatalf("unexpected values: %v", got)
	}
}

func TestUpdateDerivesTheNextValueFromTheCurrentOne(t *testing.T) {
	program := effect.NewRef[effect.Unit](1).
		FlatMap(func(ref effect.Ref[int]) effect.Effect[effect.Unit, effect.Never, int] {
			return ref.Update[effect.Unit](func(current int) int { return current * 3 }).
				FlatMap(func(effect.Unit) effect.Effect[effect.Unit, effect.Never, int] {
					return ref.UpdateAndGet[effect.Unit](func(current int) int { return current + 1 })
				})
		})

	if got := ran(t, program); got != 4 {
		t.Fatalf("expected 4, got %d", got)
	}
}

func TestGetAndUpdateReportsWhatWasThereBefore(t *testing.T) {
	program := effect.NewRef[effect.Unit]("first").
		FlatMap(func(ref effect.Ref[string]) effect.Effect[effect.Unit, effect.Never, string] {
			return ref.GetAndSet[effect.Unit]("second")
		})

	if got := ran(t, program); got != "first" {
		t.Fatalf("expected the previous value, got %q", got)
	}
}

func TestModifyDecidesAndWritesInOneStep(t *testing.T) {
	// The point of Modify: a check followed by an update is two operations and
	// two fibers can interleave between them.
	program := effect.NewRef[effect.Unit]([]string{"held"}).
		FlatMap(func(ref effect.Ref[[]string]) effect.Effect[effect.Unit, effect.Never, []bool] {
			adding := func(name string) effect.Effect[effect.Unit, effect.Never, bool] {
				return effect.Modify[effect.Unit](ref, func(current []string) ([]string, bool) {
					for _, present := range current {
						if present == name {
							return current, false
						}
					}
					return append(current, name), true
				})
			}
			return effect.All([]effect.Effect[effect.Unit, effect.Never, bool]{
				adding("held"), adding("new"), adding("new"),
			})
		})

	if got := ran(t, program); !reflect.DeepEqual(got, []bool{false, true, false}) {
		t.Fatalf("unexpected outcomes: %v", got)
	}
}

func TestOneOperationIsAtomicUnderContention(t *testing.T) {
	// A thousand parallel increments leave a thousand. A plain variable would
	// lose some, which is the whole reason this exists.
	const attempts = 1000
	program := effect.NewRef[effect.Unit](0).
		FlatMap(func(ref effect.Ref[int]) effect.Effect[effect.Unit, effect.Never, int] {
			incrementing := make([]effect.Effect[effect.Unit, effect.Never, effect.Unit], 0, attempts)
			for range attempts {
				incrementing = append(incrementing,
					ref.Update[effect.Unit](func(current int) int { return current + 1 }))
			}
			return effect.AllPar(incrementing).
				FlatMap(func([]effect.Unit) effect.Effect[effect.Unit, effect.Never, int] {
					return ref.Get[effect.Unit]()
				})
		})

	if got := ran(t, program); got != attempts {
		t.Fatalf("expected %d, lost %d", attempts, attempts-got)
	}
}

func TestAChangeIsAppliedExactlyOnce(t *testing.T) {
	// A lock-free cell would retry on contention and so would apply the change
	// more than once, which is only safe for a change that happens to be pure.
	// Nothing in Go can promise one is, so the change runs once.
	const attempts = 200
	program := effect.NewRef[effect.Unit](0).
		FlatMap(func(counted effect.Ref[int]) effect.Effect[effect.Unit, effect.Never, effect.Product[int, int]] {
			return effect.NewRef[effect.Unit](0).
				FlatMap(func(applied effect.Ref[int]) effect.Effect[effect.Unit, effect.Never, effect.Product[int, int]] {
					updating := make([]effect.Effect[effect.Unit, effect.Never, effect.Unit], 0, attempts)
					for range attempts {
						updating = append(updating, counted.Update[effect.Unit](func(current int) int {
							// Counting applications from inside the change is
							// the only way to see a retry loop from outside.
							effect.Run(context.Background(), effect.Unit{},
								applied.Update[effect.Unit](func(seen int) int { return seen + 1 }))
							return current + 1
						}))
					}
					return effect.AllPar(updating).
						FlatMap(func([]effect.Unit) effect.Effect[effect.Unit, effect.Never, effect.Product[int, int]] {
							return effect.Zip(counted.Get[effect.Unit](), applied.Get[effect.Unit]())
						})
				})
		})

	got := ran(t, program)
	if got.First != attempts || got.Second != attempts {
		t.Fatalf("expected %d updates and %d applications, got %d and %d",
			attempts, attempts, got.First, got.Second)
	}
}

func TestACellBelongsToOneInterpretation(t *testing.T) {
	// State that existed before interpretation would be shared between runs
	// and between the attempts of a retry, which is never what a program meant.
	program := effect.NewRef[effect.Unit](0).
		FlatMap(func(ref effect.Ref[int]) effect.Effect[effect.Unit, effect.Never, int] {
			return ref.UpdateAndGet[effect.Unit](func(current int) int { return current + 1 })
		})

	if first, second := ran(t, program), ran(t, program); first != 1 || second != 1 {
		t.Fatalf("expected each run to start fresh, got %d then %d", first, second)
	}
}
