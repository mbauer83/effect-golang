package effect

import (
	"fmt"

	"github.com/mbauer83/effect-golang/capability"
	runtimecore "github.com/mbauer83/effect-golang/internal/runtime"
)

// CauseReport is a structured view of a cause tree, suitable for structured
// logs and exporters that cannot consume the typed failure itself.
//
// It preserves the tree's shape, so a Then chain of cleanup failures and a Both
// of independent parallel failures remain distinguishable after export. A
// typed consumer should still use Failures, Defects and Interruptions: those
// keep the domain types, while a report is deliberately renderable.
type CauseReport struct {
	Kind     CauseKind
	Detail   string
	Stack    string
	Children []CauseReport
}

// Report produces the structured view of a cause. Like every other cause
// operation it folds rather than recurses, so an arbitrarily deep tree is safe
// to export.
func (c Cause[E]) Report() CauseReport {
	return c.Fold(CauseFolder[E, CauseReport]{
		Empty: func() CauseReport {
			return CauseReport{Kind: CauseEmpty}
		},
		Failure: func(failure E) CauseReport {
			return CauseReport{Kind: CauseFailure, Detail: fmt.Sprintf("%v", failure)}
		},
		Defect: func(defect Defect) CauseReport {
			return CauseReport{
				Kind:   CauseDefect,
				Detail: fmt.Sprintf("%v", defect.Value),
				Stack:  defect.Stack,
			}
		},
		Interruption: func(interruption Interruption) CauseReport {
			return CauseReport{
				Kind:   CauseInterrupted,
				Detail: fmt.Sprintf("%v", interruption.Cause),
			}
		},
		Then: composedReport(CauseThen),
		Both: composedReport(CauseBoth),
	})
}

func composedReport(kind CauseKind) func(CauseReport, CauseReport) CauseReport {
	return func(left CauseReport, right CauseReport) CauseReport {
		return CauseReport{Kind: kind, Children: []CauseReport{left, right}}
	}
}

// Status classifies a cause into the bounded vocabulary that runtime events and
// metric labels may carry. A defect outranks an interruption, because it
// indicates a program error rather than a requested stop.
func (c Cause[E]) Status() capability.EventStatus {
	return runtimecore.CauseStatus(c.node)
}
