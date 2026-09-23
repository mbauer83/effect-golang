package effect

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"slices"
	"time"

	"github.com/mbauer83/effect-golang/effect/capability"
	"github.com/mbauer83/effect-golang/effect/internal/outcome"
	runtimecore "github.com/mbauer83/effect-golang/effect/internal/runtime"
)

// WithName supplies a stable operation name to nested logs and runtime events.
func (fx Effect[R, E, A]) WithName(name string) Effect[R, E, A] {
	return fx.withState(func(state *runtimecore.State) *runtimecore.State {
		return state.WithName(name)
	})
}

// Annotate adds ordered structured metadata to nested logs and runtime events.
func (fx Effect[R, E, A]) Annotate(attributes ...slog.Attr) Effect[R, E, A] {
	snapshot := slices.Clone(attributes)
	return fx.withState(func(state *runtimecore.State) *runtimecore.State {
		return state.WithAttributes(snapshot)
	})
}

// WithSpan emits a named start/end pair and supplies its identity to nested
// operations. Observer failures never alter the effect's Exit.
//
// The call site is captured once, when the span is described rather than each
// time it runs, which is why source locations are recorded at named boundaries
// only and never for every combinator.
func (fx Effect[R, E, A]) WithSpan(name string, attributes ...slog.Attr) Effect[R, E, A] {
	boundary := runtimecore.SpanBoundary{
		Name:       name,
		Source:     callSite(2),
		Attributes: slices.Clone(attributes),
	}
	return suspendRuntime(func(ctx context.Context, state *runtimecore.State, _ R) Effect[R, E, A] {
		span := state.WithSpan(boundary)
		if !span.HasObserver() {
			return fx.withState(replaceState(span))
		}
		started := span.Event(capability.EventSpanStarted)
		span.Emit(ctx, started)
		return fx.withExitObserver(endSpan(started.Timestamp)).withState(replaceState(span))
	})
}

func (fx Effect[R, E, A]) withState(derive func(*runtimecore.State) *runtimecore.State) Effect[R, E, A] {
	return fromInstructions[R, E, A](&runtimecore.WithState{
		Source: fx.instructions(),
		Derive: derive,
	})
}

func (fx Effect[R, E, A]) withExitObserver(observe exitObserver) Effect[R, E, A] {
	return fromInstructions[R, E, A](&runtimecore.OnExit{
		Source:  fx.instructions(),
		Observe: observe,
	})
}

// withContext replaces the context supplied to fx, which is how a scope hands
// its own cancelable lifetime to the work it owns.
func (fx Effect[R, E, A]) withContext(ctx context.Context) Effect[R, E, A] {
	return fromInstructions[R, E, A](&runtimecore.WithContext{
		Source: fx.instructions(),
		Derive: func(context.Context) context.Context {
			return ctx
		},
	})
}

// callSite renders the file and line of the caller at the requested depth, or
// an empty string when the location is unavailable.
func callSite(depth int) string {
	_, file, line, ok := runtime.Caller(depth)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s:%d", file, line)
}

func replaceState(state *runtimecore.State) func(*runtimecore.State) *runtimecore.State {
	return func(*runtimecore.State) *runtimecore.State {
		return state
	}
}

// exitObserver is the erased hook shape shared by spans, finalizers and exit
// reification.
type exitObserver = func(runtimecore.Interpretation, outcome.Exit) outcome.Exit

func endSpan(start time.Time) exitObserver {
	return func(interpretation runtimecore.Interpretation, exit outcome.Exit) outcome.Exit {
		interpretation.State.EmitEnd(
			interpretation.Context,
			capability.EventSpanEnded,
			start,
			outcome.ExitStatus(exit),
		)
		return exit
	}
}
