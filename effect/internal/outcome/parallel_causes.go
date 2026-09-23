package outcome

import "errors"

// A parallel composition combines the causes of branches that ran at the same
// time, and must not report a branch it canceled itself as an independent
// failure.

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

// CombineParallelCauses composes two branch causes without inventing an
// independent failure.
//
// A branch that stopped only because this composition canceled it did not fail
// on its own account, so its induced interruption is dropped. Two genuinely
// independent failures are preserved with Both, in positional order.
func CombineParallelCauses(outcome PairOutcome, cancelReason error) Cause {
	left, right := outcome.Left.Cause(), outcome.Right.Cause()
	if IsInduced(right, cancelReason) {
		return left
	}
	if IsInduced(left, cancelReason) {
		return right
	}
	return left.Both(right)
}

// IsInduced reports whether a cause consists only of interruptions this
// composition requested.
func IsInduced(cause Cause, reason error) bool {
	if cause.IsEmpty() || reason == nil {
		return false
	}
	induced := true
	VisitCause(cause, func(node Cause) bool {
		switch node.Kind {
		case CauseThen, CauseBoth:
		case CauseInterrupt:
			induced = induced && errors.Is(node.Interruption.Cause, reason)
		default:
			induced = false
		}
		return induced
	})
	return induced
}

// CombineBranchCauses composes the causes of a collection composition in input
// order. A branch canceled only because a sibling failed did not fail on its
// own account, so its induced interruption is dropped.
func CombineBranchCauses(exits []Exit, cancelReason error) Cause {
	combined := Cause{}
	for _, exit := range exits {
		cause := exit.Cause()
		if IsInduced(cause, cancelReason) {
			continue
		}
		combined = combined.Both(cause)
	}
	return combined
}
