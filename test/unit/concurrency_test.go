package unit

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effecttest"
)

func meet(barrier *effecttest.Barrier, tracker *effecttest.Tracker, name string, result effect.Exit[string, string]) program {
	return effect.For[effect.Unit, string]().From(
		func(context.Context, effect.Unit) effect.Exit[string, string] {
			barrier.Arrive()
			tracker.Record(name)
			return result
		},
	)
}

func TestZipParEvaluatesBothBranchesConcurrently(t *testing.T) {
	tracker := &effecttest.Tracker{}
	meetingPoint := effecttest.NewBarrier(2)

	program := effect.ZipPar(
		meet(meetingPoint, tracker, "left", effect.ExitSuccess[string]("first")),
		meet(meetingPoint, tracker, "right", effect.ExitSuccess[string]("second")),
	)

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	value, ok := exit.Value()
	if !ok || value.First != "first" || value.Second != "second" {
		t.Fatalf("unexpected exit: %v", exit)
	}
	if len(tracker.Events()) != 2 {
		t.Fatalf("expected both branches to run, got %v", tracker.Events())
	}
}

func TestZipParPreservesTwoIndependentFailures(t *testing.T) {
	tracker := &effecttest.Tracker{}
	meetingPoint := effecttest.NewBarrier(2)

	program := effect.ZipPar(
		meet(meetingPoint, tracker, "left", effect.ExitFailure[string, string]("left failed")),
		meet(meetingPoint, tracker, "right", effect.ExitFailure[string, string]("right failed")),
	)

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed || cause.Kind() != effect.CauseBoth {
		t.Fatalf("expected Both(left, right), got %v", exit)
	}
	want := []string{"left failed", "right failed"}
	if got := cause.Failures(); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected positional failures %v, got %v", want, got)
	}
}

func TestZipParDoesNotReportInducedSiblingInterruption(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}
	work := effecttest.NewBlocker(tracker)

	program := effect.ZipPar(
		operations.Fail[string]("left failed"),
		effecttest.Block[effect.Unit, string](work, "finished"),
	)

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected failure, got %v", exit)
	}
	if cause.Kind() != effect.CauseFailure {
		t.Fatalf("expected only the real failure, got %v", cause)
	}
	if failure, _ := cause.Failure(); failure != "left failed" {
		t.Fatalf("unexpected failure: %q", failure)
	}
}

func TestZipParAwaitsTheCanceledSiblingsCleanup(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}
	work := effecttest.NewBlocker(tracker)

	sibling := effect.Scoped(func(scope effect.Scope) program {
		return effecttest.TrackResource[effect.Unit, string](scope, tracker, "sibling-handle").AndThen(effecttest.Block[effect.Unit, string](work, "finished"))
	})
	slowFailure := operations.From(func(context.Context, effect.Unit) effect.Exit[string, string] {
		work.AwaitStart()
		return effect.ExitFailure[string, string]("left failed")
	})

	effect.Run(context.Background(), effect.Unit{}, effect.ZipPar(slowFailure, sibling))

	events := tracker.Events()
	if got := tracker.Count("release sibling-handle"); got != 1 {
		t.Fatalf("expected the sibling's resource released exactly once, got %d in %v", got, events)
	}
	if tracker.Count("interrupted") != 1 {
		t.Fatalf("expected the sibling to observe interruption, got %v", events)
	}
}

func TestRaceKeepsWaitingAfterOneBranchFails(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	failed := make(chan struct{})

	// The winner cannot finish until the other branch has already failed, so a
	// first-completion implementation would report the failure instead.
	loser := operations.From(func(context.Context, effect.Unit) effect.Exit[string, string] {
		close(failed)
		return effect.ExitFailure[string, string]("fast failure")
	})
	winner := operations.From(func(context.Context, effect.Unit) effect.Exit[string, string] {
		<-failed
		return effect.ExitSuccess[string]("slow success")
	})

	program := effect.Race(loser, winner)
	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if value, ok := exit.Value(); !ok || value != "slow success" {
		t.Fatalf("expected first success to win, got %v", exit)
	}
}

func TestRaceFailsWithBothCausesWhenNeitherSucceeds(t *testing.T) {
	tracker := &effecttest.Tracker{}
	meetingPoint := effecttest.NewBarrier(2)

	program := effect.Race(
		meet(meetingPoint, tracker, "left", effect.ExitFailure[string, string]("left failed")),
		meet(meetingPoint, tracker, "right", effect.ExitFailure[string, string]("right failed")),
	)

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed || cause.Kind() != effect.CauseBoth {
		t.Fatalf("expected Both(left, right), got %v", exit)
	}
}

func TestRaceFirstLetsTheFirstCompletionWinEvenWhenItFailed(t *testing.T) {
	operations := effect.For[effect.Unit, string]()
	tracker := &effecttest.Tracker{}
	work := effecttest.NewBlocker(tracker)

	loser := effect.Scoped(func(scope effect.Scope) program {
		return effecttest.TrackResource[effect.Unit, string](scope, tracker, "loser-handle").AndThen(effecttest.Block[effect.Unit, string](work, "finished"))
	})
	fastFailure := operations.From(func(context.Context, effect.Unit) effect.Exit[string, string] {
		work.AwaitStart()
		return effect.ExitFailure[string, string]("fast failure")
	})

	exit := effect.Run(context.Background(), effect.Unit{}, effect.RaceFirst(fastFailure, loser))
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected the first completion to win, got %v", exit)
	}
	if failure, ok := cause.Failure(); !ok || failure != "fast failure" {
		t.Fatalf("expected the fast failure, got %v", cause)
	}
	if got := tracker.Count("release loser-handle"); got != 1 {
		t.Fatalf("expected the loser's resource released before RaceFirst returned, got %d", got)
	}
}

func TestZipParMergePreservesAllChannels(t *testing.T) {
	tracker := &effecttest.Tracker{}
	meetingPoint := effecttest.NewBarrier(2)
	left := effect.For[int, string]().From(
		func(_ context.Context, env int) effect.Exit[string, int] {
			meetingPoint.Arrive()
			tracker.Record("left")
			return effect.ExitSuccess[string](env * 2)
		},
	)
	right := effect.For[string, bool]().From(
		func(_ context.Context, env string) effect.Exit[bool, string] {
			meetingPoint.Arrive()
			tracker.Record("right")
			return effect.ExitSuccess[bool](env + "!")
		},
	)

	program := effect.ZipParChannels(left, right)
	exit := effect.Run(context.Background(), effect.ProductOf(21, "hello"), program)
	value, ok := exit.Value()
	if !ok || value.First != 42 || value.Second != "hello!" {
		t.Fatalf("unexpected exit: %v", exit)
	}
}

func TestParallelCompositionPropagatesCallerCancellation(t *testing.T) {
	tracker := &effecttest.Tracker{}
	stop := errors.New("caller stopped")
	ctx, cancel := context.WithCancelCause(context.Background())
	left := effecttest.NewBlocker(tracker)
	right := effecttest.NewBlocker(tracker)

	go func() {
		left.AwaitStart()
		right.AwaitStart()
		cancel(stop)
	}()

	exit := effect.Run(ctx, effect.Unit{}, effect.ZipPar(
		effecttest.Block[effect.Unit, string](left, "finished"),
		effecttest.Block[effect.Unit, string](right, "finished"),
	))
	cause, failed := exit.Cause()
	if !failed || !cause.HasInterruptsOnly() {
		t.Fatalf("expected an interruption-only cause, got %v", exit)
	}
	if got := tracker.Count("interrupted"); got != 2 {
		t.Fatalf("expected both branches to observe cancellation, got %d", got)
	}
}
