package runtime

import (
	"context"
	"time"

	"github.com/mbauer83/effect-golang/effect/capability"
)

// A lifecycle boundary is reported as a start event and an end event carrying
// the interval between them. Scopes, fibers and the runtime itself share this
// shape, so they share one pair of helpers, and every fiber the runtime starts
// is reported the same way whether an application forked it explicitly or a
// parallel combinator did.

// EmitStart reports an opening boundary and returns its clock reading, which
// the matching closing boundary uses to measure the interval.
func (state *State) EmitStart(ctx context.Context, kind capability.EventKind) time.Time {
	if !state.HasObserver() {
		return state.capabilities.Clock.Now()
	}
	event := state.Event(kind)
	state.Emit(ctx, event)
	return event.Timestamp
}

// EmitEnd reports a closing boundary with its duration and a bounded terminal
// classification.
func (state *State) EmitEnd(
	ctx context.Context,
	kind capability.EventKind,
	start time.Time,
	status capability.EventStatus,
) {
	if !state.HasObserver() {
		return
	}
	event := state.Event(kind)
	event.Duration = nonNegative(event.Timestamp.Sub(start))
	event.Status = status
	state.Emit(ctx, event)
}

// EmitMark reports a boundary that has neither a duration nor an outcome of its
// own, such as a scope beginning to close or a resource being acquired. The
// resource itself is never included.
func (state *State) EmitMark(ctx context.Context, kind capability.EventKind) {
	if !state.HasObserver() {
		return
	}
	state.Emit(ctx, state.Event(kind))
}

// nonNegative guards against a clock that appears to move backwards, which a
// test clock or a coarse platform timer can both produce.
func nonNegative(duration time.Duration) time.Duration {
	if duration < 0 {
		return 0
	}
	return duration
}
