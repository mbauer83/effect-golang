package effect_test

import (
	"context"
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

	program := query.ProvideLayer(layer)
	exit := effect.Run(context.Background(), config{DSN: "postgres://db"}, program)
	value, ok := exit.Value()
	if !ok || value != "postgres://db/users" {
		t.Fatalf("unexpected exit: %#v", exit)
	}
}
