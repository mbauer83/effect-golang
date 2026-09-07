package effect_test

import (
	"context"
	"testing"

	effect "github.com/mbauer83/effect-golang"
)

func TestSequentialConveniencesCompose(t *testing.T) {
	observed := 0
	program := effect.Succeed[effect.Unit, string](20).
		Tap(func(value int) effect.Effect[effect.Unit, string, effect.Unit] {
			return effect.From(func(context.Context, effect.Unit) effect.Exit[string, effect.Unit] {
				observed = value
				return effect.ExitSuccess[string](effect.Unit{})
			})
		}).
		As(22).
		AndThen(effect.Succeed[effect.Unit, string](42))

	if observed != 0 {
		t.Fatal("effect evaluated before Run")
	}
	exit := effect.Run(context.Background(), effect.Unit{}, program)
	value, ok := exit.Value()
	if !ok || value != 42 || observed != 20 {
		t.Fatalf("unexpected result: value=%d observed=%d exit=%v", value, observed, exit)
	}
}

func TestFlattenRunsInnerEffect(t *testing.T) {
	inner := effect.Succeed[effect.Unit, string](42)
	outer := effect.Succeed[effect.Unit, string](inner)

	exit := effect.Run(context.Background(), effect.Unit{}, effect.Flatten(outer))
	value, ok := exit.Value()
	if !ok || value != 42 {
		t.Fatalf("unexpected exit: %v", exit)
	}
}

func TestCheckInterruptObservesCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	exit := effect.Run(ctx, effect.Unit{}, effect.CheckInterrupt[effect.Unit, string]())
	cause, ok := exit.Cause()
	if !ok || cause.Kind() != effect.CauseInterrupted {
		t.Fatalf("expected interruption, got %v", exit)
	}
}
