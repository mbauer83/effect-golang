package effect_test

import (
	"strconv"
	"strings"
	"testing"

	effect "github.com/mbauer83/effect-golang"
)

func TestEitherRightBiasedMonad(t *testing.T) {
	result := effect.Right[string](21).
		Map(func(value int) int { return value * 2 }).
		FlatMap(func(value int) effect.Either[string, string] {
			return effect.Right[string](strconv.Itoa(value))
		})

	value, ok := result.RightValue()
	if !ok || value != "42" {
		t.Fatalf("expected Right(42), got %#v", result)
	}
}

func TestEitherPreservesLeft(t *testing.T) {
	result := effect.Left[string, int]("nope").Map(func(value int) int {
		t.Fatal("Map must not evaluate the Right function for Left")
		return value
	})

	failure, ok := result.LeftValue()
	if !ok || failure != "nope" {
		t.Fatalf("expected Left(nope), got %#v", result)
	}
}

func TestProductMapsIndependently(t *testing.T) {
	product := effect.ProductOf(2, "3").
		MapFirst(func(value int) int { return value * 10 }).
		MapSecond(func(value string) int {
			parsed, _ := strconv.Atoi(value)
			return parsed
		})

	if product.First != 20 || product.Second != 3 {
		t.Fatalf("unexpected product: %#v", product)
	}
}

func TestEitherEliminationAndReorientation(t *testing.T) {
	right := effect.Right[string](7)
	left := effect.Left[string, int]("rejected")

	if !right.IsRight() || right.IsLeft() || left.IsRight() {
		t.Fatal("expected the discriminators to agree with the constructors")
	}
	if value, ok := right.Swap().LeftValue(); !ok || value != 7 {
		t.Fatalf("expected Swap to move Right to Left, got %v", right.Swap())
	}
	if value, ok := left.Swap().RightValue(); !ok || value != "rejected" {
		t.Fatalf("expected Swap to move Left to Right, got %v", left.Swap())
	}

	tagged := left.MapLeft(func(reason string) int { return len(reason) })
	if value, ok := tagged.LeftValue(); !ok || value != len("rejected") {
		t.Fatalf("expected MapLeft to rewrite only the Left side, got %v", tagged)
	}
	if mapped := right.MapLeft(func(string) int { return -1 }); !mapped.IsRight() {
		t.Fatalf("expected MapLeft to preserve a Right, got %v", mapped)
	}

	bimapped := left.Bimap(strings.ToUpper, func(value int) bool { return value > 0 })
	if value, ok := bimapped.LeftValue(); !ok || value != "REJECTED" {
		t.Fatalf("expected Bimap to apply the active side only, got %v", bimapped)
	}
}

func TestProductBimapTransformsBothComponents(t *testing.T) {
	pair := effect.ProductOf("count", 3)
	mapped := pair.Bimap(strings.ToUpper, func(value int) bool { return value > 2 })

	if mapped.First != "COUNT" || !mapped.Second {
		t.Fatalf("unexpected product: %#v", mapped)
	}
}
