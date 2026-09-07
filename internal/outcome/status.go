package outcome

import "github.com/mbauer83/effect-golang/capability"

// ExitStatus classifies a terminal exit into the bounded vocabulary that
// runtime events and metric labels may safely carry.
func ExitStatus(exit Exit) capability.EventStatus {
	if exit.Succeeded() {
		return capability.EventStatusSuccess
	}
	return CauseStatus(exit.Cause())
}

// CauseStatus reports the most severe classification present in a cause tree.
// A defect outranks an interruption because it indicates a program error
// rather than a requested stop.
func CauseStatus(cause Cause) capability.EventStatus {
	status := capability.EventStatusFailure
	VisitCause(cause, func(node Cause) bool {
		switch node.Kind {
		case CauseDefect:
			status = capability.EventStatusDefect
			return false
		case CauseInterrupted:
			status = capability.EventStatusInterrupted
		}
		return true
	})
	return status
}

// CleanupStatus classifies a lifetime's release outcome.
func CleanupStatus(cleanup Cause) capability.EventStatus {
	if cleanup.IsEmpty() {
		return capability.EventStatusSuccess
	}
	return CauseStatus(cleanup)
}
