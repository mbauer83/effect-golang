package effect_test

import (
	"context"
	"errors"
	"testing"

	effect "github.com/mbauer83/effect-golang"
)

type dbEnv struct{ prefix string }
type mailEnv struct{ suffix string }

type dbError struct{ message string }
type mailError struct{ message string }

func TestFlatMapKeepsSharedChannelsFlat(t *testing.T) {
	first := effect.Succeed[dbEnv, dbError](20)
	program := first.FlatMap(func(value int) effect.Effect[dbEnv, dbError, int] {
		return effect.Succeed[dbEnv, dbError](value + 22)
	})

	exit := effect.Run(context.Background(), dbEnv{}, program)
	value, ok := exit.Value()
	if !ok || value != 42 {
		t.Fatalf("expected 42, got %#v", exit)
	}
}

func TestFlatMapMergePreservesThreeChannels(t *testing.T) {
	load := effect.FromEither(func(_ context.Context, env dbEnv) effect.Either[dbError, string] {
		return effect.Right[dbError](env.prefix + "user")
	})

	program := load.FlatMapMerge(func(user string) effect.Effect[mailEnv, mailError, string] {
		return effect.FromEither(func(_ context.Context, env mailEnv) effect.Either[mailError, string] {
			return effect.Right[mailError](user + env.suffix)
		})
	})

	env := effect.ProductOf(dbEnv{prefix: "db:"}, mailEnv{suffix: ":mail"})
	exit := effect.Run(context.Background(), env, program)
	value, ok := exit.Value()
	if !ok || value != "db:user:mail" {
		t.Fatalf("unexpected exit: %#v", exit)
	}
}

func TestFlatMapMergeTagsFailureOrigin(t *testing.T) {
	load := effect.Fail[dbEnv, string](dbError{message: "db down"})
	program := load.FlatMapMerge(func(string) effect.Effect[mailEnv, mailError, string] {
		t.Fatal("second effect must not run")
		return effect.Succeed[mailEnv, mailError]("")
	})

	exit := effect.Run(context.Background(), effect.ProductOf(dbEnv{}, mailEnv{}), program)
	cause, ok := exit.Cause()
	if !ok {
		t.Fatal("expected failure")
	}
	combined, ok := cause.Failure()
	if !ok || !combined.IsLeft() {
		t.Fatalf("expected typed Left failure, got %#v", cause)
	}
}

func TestPanicBecomesDefect(t *testing.T) {
	program := effect.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		panic("boom")
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, ok := exit.Cause()
	if !ok || cause.Kind() != effect.CauseDefect {
		t.Fatalf("expected defect, got %#v", exit)
	}
	defect, _ := cause.Defect()
	if defect.Value != "boom" || defect.Stack == "" {
		t.Fatalf("unexpected defect: %#v", defect)
	}
}

func TestCanceledContextBecomesInterruption(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	program := effect.Succeed[effect.Unit, string](42)
	exit := effect.Run(ctx, effect.Unit{}, program)
	cause, ok := exit.Cause()
	if !ok || cause.Kind() != effect.CauseInterrupted {
		t.Fatalf("expected interruption, got %#v", exit)
	}
	interruption, _ := cause.Interruption()
	if !errors.Is(interruption, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", interruption)
	}
}

func TestCatchAllHandlesTypedFailureOnly(t *testing.T) {
	program := effect.Fail[effect.Unit, int]("bad").CatchAll(func(message string) effect.Effect[effect.Unit, int, int] {
		return effect.Succeed[effect.Unit, int](len(message))
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	value, ok := exit.Value()
	if !ok || value != 3 {
		t.Fatalf("unexpected exit: %#v", exit)
	}
}

func TestProvideRemovesEnvironment(t *testing.T) {
	program := effect.FromEither(func(_ context.Context, env dbEnv) effect.Either[dbError, string] {
		return effect.Right[dbError](env.prefix + "user")
	}).Provide(dbEnv{prefix: "db:"})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	value, ok := exit.Value()
	if !ok || value != "db:user" {
		t.Fatalf("unexpected exit: %#v", exit)
	}
}
