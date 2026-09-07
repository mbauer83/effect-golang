package runtime

import (
	"context"
	"errors"
	"fmt"
)

// Side names one branch of a two-branch concurrent composition.
type Side uint8

const (
	// NeitherSide means no branch had completed when the composition settled.
	NeitherSide Side = iota
	LeftSide
	RightSide
)

// PairOutcome holds both branches' terminal exits in positional order, so a
// composed cause can preserve which side failed, plus which branch completed
// first for the compositions that care.
type PairOutcome struct {
	Left  Exit
	Right Exit
	First Side
}

// SettlePolicy decides whether one branch's terminal exit already settles the
// whole composition, making the other branch's result unnecessary.
type SettlePolicy func(Exit) bool

// RunPair evaluates two branches concurrently inside a private scope.
//
// It returns only after both branches have terminated and the private scope has
// released everything they owned, so a discarded branch is never still running
// or finalizing when the composition completes. Cancellation reaches both
// branches automatically, because the private scope's context is a child of the
// caller's; no separate cancellation case is needed and none is wanted, since
// abandoning a running branch is exactly what this must not do.
func RunPair(
	interpretation Interpretation,
	left func(context.Context, *State) Exit,
	right func(context.Context, *State) Exit,
	settle SettlePolicy,
	reason error,
) (PairOutcome, Cause) {
	scope := NewScope(interpretation.Context)
	runner, unavailable := startPair(scope, interpretation.State, left, right)
	if unavailable != nil {
		return PairOutcome{}, *unavailable
	}

	runner.reason = reason
	runner.wait(settle)
	cleanup := scope.Close(interpretation.Context, Success(runner.outcome), reason)
	return runner.outcome, cleanup
}

type pairRunner struct {
	left      *Fiber
	right     *Fiber
	leftDone  <-chan struct{}
	rightDone <-chan struct{}
	outcome   PairOutcome
	reason    error
}

func startPair(
	scope *Scope,
	state *State,
	left func(context.Context, *State) Exit,
	right func(context.Context, *State) Exit,
) (*pairRunner, *Cause) {
	leftFiber, leftStarted := StartFiber(scope, scope.Context(), state, left)
	rightFiber, rightStarted := StartFiber(scope, scope.Context(), state, right)
	if !leftStarted || !rightStarted {
		// A freshly opened scope always accepts work, so this is unreachable
		// unless the runtime itself is inconsistent.
		failure := DieCause(Defect{Value: fmt.Errorf("effect: fresh scope rejected a parallel branch")})
		return nil, &failure
	}
	return &pairRunner{
		left:      leftFiber,
		right:     rightFiber,
		leftDone:  leftFiber.Done(),
		rightDone: rightFiber.Done(),
	}, nil
}

// wait blocks until both branches have terminated. A nil completion channel
// removes a finished branch from the select, which is Go's idiom for a
// disabled case.
func (runner *pairRunner) wait(settle SettlePolicy) {
	for runner.leftDone != nil || runner.rightDone != nil {
		select {
		case <-runner.leftDone:
			runner.collectLeft()
			if settle(runner.outcome.Left) {
				runner.discardRight()
			}
		case <-runner.rightDone:
			runner.collectRight()
			if settle(runner.outcome.Right) {
				runner.discardLeft()
			}
		}
	}
}

func (runner *pairRunner) collectLeft() {
	runner.leftDone = nil
	runner.outcome.Left, _ = runner.left.Poll()
	runner.recordFirst(LeftSide)
}

func (runner *pairRunner) collectRight() {
	runner.rightDone = nil
	runner.outcome.Right, _ = runner.right.Poll()
	runner.recordFirst(RightSide)
}

func (runner *pairRunner) recordFirst(side Side) {
	if runner.outcome.First == NeitherSide {
		runner.outcome.First = side
	}
}

// discardRight cancels the branch whose result is no longer needed and waits
// for its cleanup, so the composition never outruns its own children.
func (runner *pairRunner) discardRight() {
	if runner.rightDone == nil {
		return
	}
	runner.rightDone = nil
	runner.outcome.Right = runner.right.Interrupt(runner.reason)
}

func (runner *pairRunner) discardLeft() {
	if runner.leftDone == nil {
		return
	}
	runner.leftDone = nil
	runner.outcome.Left = runner.left.Interrupt(runner.reason)
}

// CombineParallelCauses composes two branch causes without inventing an
// independent failure.
//
// A branch that stopped only because this composition canceled it did not fail
// on its own account, so its induced interruption is dropped. Two genuinely
// independent failures are preserved with Both, in positional order.
func CombineParallelCauses(outcome PairOutcome, induced error) Cause {
	left, right := outcome.Left.Cause(), outcome.Right.Cause()
	if WasInduced(right, induced) {
		return left
	}
	if WasInduced(left, induced) {
		return right
	}
	return left.Both(right)
}

// WasInduced reports whether a cause consists only of interruptions this
// composition requested.
func WasInduced(cause Cause, reason error) bool {
	if cause.IsEmpty() || reason == nil {
		return false
	}
	induced := true
	VisitCause(cause, func(node Cause) bool {
		switch node.Kind {
		case CauseThen, CauseBoth:
		case CauseInterrupted:
			induced = induced && errors.Is(node.Interruption.Cause, reason)
		default:
			induced = false
		}
		return induced
	})
	return induced
}
