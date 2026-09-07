package runtime

import (
	"context"
	"fmt"
	"github.com/mbauer83/effect-golang/effect/internal/lifetime"
	"github.com/mbauer83/effect-golang/effect/internal/outcome"
)

// SettlePolicy decides whether one branch's terminal exit already settles the
// whole composition, making the other branch's result unnecessary.
type SettlePolicy func(outcome.Exit) bool

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
	left func(context.Context, *State) outcome.Exit,
	right func(context.Context, *State) outcome.Exit,
	settle SettlePolicy,
	reason error,
) (outcome.PairOutcome, outcome.Cause) {
	scope := lifetime.NewScope(interpretation.Context)
	runner, unavailable := startPair(scope, interpretation.State, left, right)
	if unavailable != nil {
		return outcome.PairOutcome{}, *unavailable
	}

	runner.reason = reason
	runner.wait(settle)
	cleanup := scope.Close(interpretation.Context, outcome.Success(runner.outcome), reason)
	return runner.outcome, cleanup
}

type pairRunner struct {
	left      *lifetime.Fiber
	right     *lifetime.Fiber
	leftDone  <-chan struct{}
	rightDone <-chan struct{}
	outcome   outcome.PairOutcome
	reason    error
}

func startPair(
	scope *lifetime.Scope,
	state *State,
	left func(context.Context, *State) outcome.Exit,
	right func(context.Context, *State) outcome.Exit,
) (*pairRunner, *outcome.Cause) {
	leftFiber, leftStarted := StartFiber(scope, scope.Context(), state, left)
	rightFiber, rightStarted := StartFiber(scope, scope.Context(), state, right)
	if !leftStarted || !rightStarted {
		// A freshly opened scope always accepts work, so this is unreachable
		// unless the runtime itself is inconsistent.
		failure := outcome.DieCause(outcome.Defect{Value: fmt.Errorf("effect: fresh scope rejected a parallel branch")})
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
	runner.recordFirst(outcome.LeftSide)
}

func (runner *pairRunner) collectRight() {
	runner.rightDone = nil
	runner.outcome.Right, _ = runner.right.Poll()
	runner.recordFirst(outcome.RightSide)
}

func (runner *pairRunner) recordFirst(side outcome.Side) {
	if runner.outcome.First == outcome.NeitherSide {
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
