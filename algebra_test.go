package effect_test

import (
	"strconv"
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
