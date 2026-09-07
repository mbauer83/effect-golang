package unit

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
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

	program := effect.FlatMapMerge(load, func(user string) effect.Effect[mailEnv, mailError, string] {
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
	program := effect.FlatMapMerge(load, func(string) effect.Effect[mailEnv, mailError, string] {
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
	if !errors.Is(interruption.Cause, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", interruption)
	}
}

func TestCatchAllHandlesTypedFailureOnly(t *testing.T) {
	program := effect.Fail[effect.Unit, int]("bad").CatchAll(func(message string) effect.Effect[effect.Unit, string, int] {
		return effect.Succeed[effect.Unit, string](len(message))
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

func TestContramapEnvAdaptsALargerEnvironment(t *testing.T) {
	type wide struct {
		Prefix string
		Unused int
	}
	narrow := effect.For[string, string]().From(
		func(_ context.Context, prefix string) effect.Exit[string, string] {
			return effect.ExitSuccess[string](prefix + "value")
		},
	)

	adapted := narrow.ContramapEnv(func(env wide) string { return env.Prefix })
	exit := effect.Run(context.Background(), wide{Prefix: "item:", Unused: 7}, adapted)
	if value, ok := exit.Value(); !ok || value != "item:value" {
		t.Fatalf("unexpected exit: %v", exit)
	}
}

func TestMapErrorRewritesEveryFailureLeafAndLeavesOtherNodesAlone(t *testing.T) {
	composite := effect.FailCause("left").Both(
		effect.DieCause[string](effect.Defect{Value: "boom"}).Then(effect.FailCause("right")),
	)
	program := effect.FailWithCause[effect.Unit, int](composite).
		MapError(func(reason string) int { return len(reason) })

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed || cause.Kind() != effect.CauseBoth {
		t.Fatalf("expected the structure preserved, got %v", exit)
	}
	if failures := cause.Failures(); !reflect.DeepEqual(failures, []int{4, 5}) {
		t.Fatalf("expected every failure rewritten, got %#v", failures)
	}
	if defects := cause.Defects(); len(defects) != 1 || defects[0].Value != "boom" {
		t.Fatalf("expected the defect untouched, got %#v", defects)
	}
}

func TestZipMergeRetainsAllThreeChannelsSequentially(t *testing.T) {
	left := effect.For[int, string]().From(
		func(_ context.Context, env int) effect.Exit[string, int] {
			return effect.ExitSuccess[string](env * 2)
		},
	)
	right := effect.For[string, bool]().From(
		func(_ context.Context, env string) effect.Exit[bool, string] {
			return effect.ExitSuccess[bool](env + "!")
		},
	)

	merged := effect.ZipMerge(left, right)
	exit := effect.Run(context.Background(), effect.ProductOf(21, "hi"), merged)
	value, ok := exit.Value()
	if !ok || value.First != 42 || value.Second != "hi!" {
		t.Fatalf("unexpected exit: %v", exit)
	}
}

func TestTryClassifiesAnExternalErrorIntoTheTypedChannel(t *testing.T) {
	broken := errors.New("dial tcp: connection refused")
	program := effect.Try(
		func(context.Context, effect.Unit) (int, error) { return 0, broken },
		func(err error) string { return "unreachable: " + err.Error() },
	)

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected a typed failure, got %v", exit)
	}
	failure, _ := cause.Failure()
	if failure != "unreachable: "+broken.Error() {
		t.Fatalf("unexpected classification: %q", failure)
	}

	succeeding := effect.Try(
		func(context.Context, effect.Unit) (int, error) { return 5, nil },
		func(error) string { return "unreachable" },
	)
	if value, ok := effect.Run(context.Background(), effect.Unit{}, succeeding).Value(); !ok || value != 5 {
		t.Fatal("expected Try to pass a successful value through")
	}
}
