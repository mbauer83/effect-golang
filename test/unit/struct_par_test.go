package unit

// Effects of different types run at once into one struct.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang/effect"
)

type dashboard struct {
	Name   string
	Visits int
}

func TestEffectsOfDifferentTypesFillOneStructAtOnce(t *testing.T) {
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	slow := func(value int) effect.Effect[effect.Unit, error, int] {
		return effect.Sleep[effect.Unit, error](50 * time.Millisecond).As(value)
	}
	started := time.Now()
	board, filled := runtime.Run(context.Background(), effect.Unit{}, effect.StructPar(
		effect.FieldOf(effect.Sleep[effect.Unit, error](50*time.Millisecond).As("ada"), func(board *dashboard, name string) { board.Name = name }),
		effect.FieldOf(slow(3), func(board *dashboard, visits int) { board.Visits = visits }),
	)).Value()
	if !filled || board != (dashboard{Name: "ada", Visits: 3}) {
		t.Fatalf("expected both fields, got %+v", board)
	}
	if took := time.Since(started); took > 90*time.Millisecond {
		t.Errorf("expected the two to run at once, took %s", took)
	}
}

func TestAFieldThatFailsFailsTheStruct(t *testing.T) {
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	refused := errors.New("no visits")
	cause, failed := runtime.Run(context.Background(), effect.Unit{}, effect.StructPar(
		effect.FieldOf(effect.Succeed[effect.Unit, error]("ada"), func(board *dashboard, name string) { board.Name = name }),
		effect.FieldOf(effect.Fail[effect.Unit, int](refused), func(board *dashboard, visits int) { board.Visits = visits }),
	)).Cause()
	why, single := cause.Failure()
	if !failed || !single || !errors.Is(why, refused) {
		t.Errorf("expected the field's failure, got %v", cause)
	}
}
