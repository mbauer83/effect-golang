package effect_test

import (
	"context"
	"strings"
	"testing"

	effect "github.com/mbauer83/effect-golang"
)

type config struct{ DSN string }
type database struct{ DSN string }
type buildError string
type queryError string

func TestProvideLayer(t *testing.T) {
	layer := effect.LayerFromEffect(effect.FromEither(func(_ context.Context, cfg config) effect.Either[buildError, database] {
		return effect.Right[buildError](database{DSN: cfg.DSN})
	}))

	query := effect.FromEither(func(_ context.Context, db database) effect.Either[queryError, string] {
		return effect.Right[queryError](db.DSN + "/users")
	})

	program := effect.ProvideLayer(query, layer)
	exit := effect.Run(context.Background(), config{DSN: "postgres://db"}, program)
	value, ok := exit.Value()
	if !ok || value != "postgres://db/users" {
		t.Fatalf("unexpected exit: %#v", exit)
	}
}

type stageOneError struct{ Reason string }
type stageTwoError struct{ Reason string }

func TestThenLayersFeedsOneLayerIntoTheNextAndTagsFailures(t *testing.T) {
	prefix := effect.LayerFromEffect(effect.Succeed[effect.Unit, stageOneError]("item:"))
	rejecting := effect.LayerFromEffect(
		effect.Fail[string, int](stageTwoError{Reason: "no catalogue"}),
	)

	chained := effect.ThenLayers(prefix, rejecting)
	exit := effect.Run(context.Background(), effect.Unit{}, chained.Build())

	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected the second layer to fail, got %v", exit)
	}
	tagged, isLeaf := cause.Failure()
	if !isLeaf {
		t.Fatalf("expected a single failure, got %v", cause)
	}
	if failure, ok := tagged.RightValue(); !ok || failure.Reason != "no catalogue" {
		t.Fatalf("expected the second layer's failure tagged Right, got %v", tagged)
	}
}

func TestZipLayersBuildsIndependentLayersAndKeepsBothOutputs(t *testing.T) {
	left := effect.LayerFromEffect(effect.Succeed[int, stageOneError]("prefix"))
	right := effect.LayerFromEffect(effect.Succeed[string, stageTwoError](42))

	combined := effect.ZipLayers(left, right)
	exit := effect.Run(context.Background(), effect.ProductOf(1, "seed"), combined.Build())

	value, ok := exit.Value()
	if !ok || value.First != "prefix" || value.Second != 42 {
		t.Fatalf("unexpected exit: %v", exit)
	}
}

func TestLayerMapTransformsTheProducedEnvironment(t *testing.T) {
	layer := effect.LayerFromEffect(effect.Succeed[effect.Unit, stageOneError](3)).
		Map(func(count int) string { return strings.Repeat("x", count) })

	exit := effect.Run(context.Background(), effect.Unit{}, layer.Build())
	if value, ok := exit.Value(); !ok || value != "xxx" {
		t.Fatalf("unexpected exit: %v", exit)
	}
}
