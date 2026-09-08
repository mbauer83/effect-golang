package unit

// A stream's failure channel. A stream produced by one layer is consumed by
// another whose failure type is its own, and a stream's failures are not
// reachable after it is built -- so the adaptation has to be a transform like
// any other.

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
)

func TestMapStreamErrorChangesTheFailureChannel(t *testing.T) {
	// A stream produced by one layer is consumed by another whose failure type
	// is its own. Without this, a Stream[R, TransportFault, A] could not be used
	// where the application's own E is wanted, because a stream's failures are
	// not reachable after it is built.
	failing := effect.StreamFail[effect.Unit, int]("the source stopped")
	adapted := effect.MapStreamError(failing, func(reason string) int { return len(reason) })

	exit := effect.Run(context.Background(), effect.Unit{}, effect.RunCollect(adapted))
	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected the failure to survive the mapping, got %+v", exit)
	}
	if failures := cause.Failures(); len(failures) != 1 || failures[0] != len("the source stopped") {
		t.Fatalf("expected the mapped failure, got %+v", cause)
	}
}

func TestMapStreamErrorMapsAcquisitionAndPullAlike(t *testing.T) {
	// Either can fail, and a consumer that only saw one of them would be
	// surprised by the other.
	acquisitionFailed := effect.StreamFromResource(
		func(effect.Scope) effect.Effect[effect.Unit, string, int] {
			return streamOperations.Fail[int]("the source could not be opened")
		},
		func(int) effect.Stream[effect.Unit, string, int] {
			return streamOperations.StreamOf(1)
		},
	)
	pullFailed := effect.ConcatStreams(
		streamOperations.StreamOf(1),
		effect.StreamFail[effect.Unit, int]("the source stopped"),
	)

	for stage, stream := range map[string]effect.Stream[effect.Unit, string, int]{
		"acquisition": acquisitionFailed,
		"pull":        pullFailed,
	} {
		adapted := effect.MapStreamError(stream, func(reason string) string { return "mapped: " + reason })
		exit := effect.Run(context.Background(), effect.Unit{}, effect.RunCollect(adapted))
		cause, failed := exit.Cause()
		if !failed {
			t.Errorf("%s: expected a failure, got %+v", stage, exit)
			continue
		}
		failures := cause.Failures()
		if len(failures) != 1 || !strings.HasPrefix(failures[0], "mapped: ") {
			t.Errorf("%s: expected the mapped failure, got %+v", stage, cause)
		}
	}
}

func TestMapStreamErrorLeavesASucceedingStreamAlone(t *testing.T) {
	adapted := effect.MapStreamError(
		streamOperations.StreamOf(1, 2, 3),
		func(string) int { return 0 },
	)
	exit := effect.Run(context.Background(), effect.Unit{}, effect.RunCollect(adapted))
	if got, ok := exit.Value(); !ok || !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("unexpected values: %v", exit)
	}
}
