// Package diagnostics is a complete program showing what a compositional Cause
// preserves when several things go wrong at once.
//
// Two stages run in parallel. One fails with a typed domain error and its
// cleanup then defects; the other panics and its cleanup defects too. A scalar
// error model would keep one of those four facts. The runtime keeps all of
// them, and keeps the distinction between "these happened in sequence" and
// "these happened independently".
package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/mbauer83/effect-golang/effect"
)

// StageError is the program's expected failure type.
type StageError struct {
	Stage  string
	Reason string
}

func (failure StageError) Error() string {
	return fmt.Sprintf("%s: %s", failure.Stage, failure.Reason)
}

// ErrCleanupFailed is the release failure both stages report.
var ErrCleanupFailed = errors.New("cleanup failed")

type stageProgram = effect.Effect[effect.Unit, StageError, string]

// Program composes two independently failing stages so their causes compose
// rather than compete.
//
// The stages meet at a rendezvous before either of them fails, so both failures
// are genuinely independent on every run. Without it the faster stage would
// cancel the slower one and the example would demonstrate induced interruption
// instead: correct runtime behaviour, but a different lesson. The rendezvous is
// allocated per interpretation, so the returned Effect stays reusable.
func Program() stageProgram {
	return effect.Suspend(func() stageProgram {
		meeting := newRendezvous(2)
		return effect.ZipPar(rejectingStage(meeting), panickingStage(meeting)).
			Map(func(both effect.Product[string, string]) string {
				return both.First + both.Second
			})
	})
}

// rendezvous releases each participant only once all of them have arrived.
type rendezvous struct {
	participants sync.WaitGroup
}

func newRendezvous(participants int) *rendezvous {
	meeting := &rendezvous{}
	meeting.participants.Add(participants)
	return meeting
}

func (meeting *rendezvous) arrive() {
	meeting.participants.Done()
	meeting.participants.Wait()
}

// rejectingStage fails with a typed domain error, then fails to clean up.
func rejectingStage(meeting *rendezvous) stageProgram {
	operations := effect.For[effect.Unit, StageError]()
	return operations.
		From(func(context.Context, effect.Unit) effect.Exit[StageError, string] {
			meeting.arrive()
			return effect.ExitFailure[StageError, string](StageError{
				Stage:  "validate",
				Reason: "record is not well formed",
			})
		}).
		Ensuring(failingCleanup("validate")).
		Named("validate")
}

// panickingStage panics, which the runtime records as a defect rather than
// letting it escape as a typed failure, and then fails to clean up.
func panickingStage(meeting *rendezvous) stageProgram {
	operations := effect.For[effect.Unit, StageError]()
	return operations.
		From(func(context.Context, effect.Unit) effect.Exit[StageError, string] {
			meeting.arrive()
			panic("index out of range in stage 'transform'")
		}).
		Ensuring(failingCleanup("transform")).
		Named("transform")
}

func failingCleanup(stage string) effect.Effect[effect.Unit, effect.Never, effect.Unit] {
	return effect.Release[effect.Unit](func(context.Context) error {
		return fmt.Errorf("%s: %w", stage, ErrCleanupFailed)
	})
}

// Diagnosis is one outcome rendered for a human reader and for a structured
// sink at the same time.
type Diagnosis struct {
	Rendered string
	Report   effect.CauseReport
	Status   effect.EventStatus
}

// Diagnose eliminates an exit with a typed fold rather than by probing it.
//
// Neither renderer needs to parse the other's output, and no question about the
// outcome needs display text to answer it: the machine-readable accessors on
// Cause report the same facts directly.
func Diagnose(exit effect.Exit[StageError, string]) Diagnosis {
	return exit.Fold(
		func(cause effect.Cause[StageError]) Diagnosis {
			return Diagnosis{
				Rendered: cause.String(),
				Report:   cause.Report(),
				Status:   cause.Status(),
			}
		},
		func(value string) Diagnosis {
			return Diagnosis{
				Rendered: "Success(" + value + ")",
				Status:   effect.EventStatusSuccess,
			}
		},
	)
}
