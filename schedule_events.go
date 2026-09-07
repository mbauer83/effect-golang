package effect

import (
	"time"

	"github.com/mbauer83/effect-golang/capability"
	runtimecore "github.com/mbauer83/effect-golang/internal/runtime"
)

// Scheduling event construction. Scope, fiber and runtime lifecycle events are
// built by the runtime state itself, because it owns the identities and the
// clock they need.
//
// Every event below carries only bounded metadata: identities, an attempt
// number, a delay, a duration and a terminal classification. No environment,
// failure value or successful result is ever attached, so an observer cannot
// become a channel for sensitive data and an exporter cannot derive an
// unbounded metric label from one.

// The scheduling events below carry only bounded metadata: an attempt number,
// the selected delay and a terminal classification. No environment, failure
// value or successful result is attached, so an observer cannot become a
// channel for sensitive data or unbounded metric labels.

func retryScheduledEvent(attempt uint64, delay time.Duration) attemptEvent {
	return attemptEventOf(capability.EventRetryScheduled, attempt, delay, capability.EventStatusFailure)
}

func retryExhaustedEvent(attempt uint64) attemptEvent {
	return attemptEventOf(capability.EventRetryExhausted, attempt, 0, capability.EventStatusFailure)
}

func retrySucceededEvent(attempt uint64) attemptEvent {
	return attemptEventOf(capability.EventRetrySucceeded, attempt, 0, capability.EventStatusSuccess)
}

func repeatScheduledEvent(attempt uint64, delay time.Duration) attemptEvent {
	return attemptEventOf(capability.EventRepeatScheduled, attempt, delay, capability.EventStatusSuccess)
}

func repeatCompletedEvent(attempt uint64) attemptEvent {
	return attemptEventOf(capability.EventRepeatCompleted, attempt, 0, capability.EventStatusSuccess)
}

func attemptEventOf(
	kind capability.EventKind,
	attempt uint64,
	delay time.Duration,
	status capability.EventStatus,
) attemptEvent {
	return func(state *runtimecore.State) capability.RuntimeEvent {
		event := state.Event(kind)
		event.Attempt = attempt
		event.Delay = delay
		event.Status = status
		return event
	}
}
