package runtime

import (
	"context"
	"fmt"
	"github.com/mbauer83/effect-golang/internal/lifetime"
	"log/slog"
	"slices"
	"sync/atomic"

	"github.com/mbauer83/effect-golang/capability"
)

// State contains runtime-owned services and observation metadata passed
// explicitly through evaluation. It is never smuggled through context.Value.
type State struct {
	capabilities capability.Set
	identifiers  *identifiers
	metadata     Metadata
	scope        *lifetime.Scope
	root         *lifetime.Scope
	ledger       *lifetime.Ledger
}

// Metadata is inherited by nested effect evaluation.
type Metadata struct {
	Operation   string
	Source      string
	FiberID     uint64
	ParentFiber uint64
	SpanID      uint64
	ParentID    uint64
	Attributes  []slog.Attr
}

// identifiers is shared by every state derived from one runtime so spans and
// fibers receive stable, unique, monotonically increasing identities.
type identifiers struct {
	spans  atomic.Uint64
	fibers atomic.Uint64
}

// NewState constructs runtime state from a complete capability set and the
// runtime's root scope. A nil ledger disables debug tracking.
func NewState(capabilities capability.Set, root *lifetime.Scope, ledger *lifetime.Ledger) *State {
	return &State{
		capabilities: capabilities,
		identifiers:  &identifiers{},
		scope:        root,
		root:         root,
		ledger:       ledger,
	}
}

// Ledger returns the runtime's debug work ledger, which is nil unless the
// runtime enabled tracking. Its methods tolerate that nil.
func (state *State) Ledger() *lifetime.Ledger {
	return state.ledger
}

// Capabilities returns the immutable capability set.
func (state *State) Capabilities() capability.Set {
	return state.capabilities
}

// Scope returns the lifetime boundary that currently owns forked work and
// acquired resources.
func (state *State) Scope() *lifetime.Scope {
	return state.scope
}

// Root returns the runtime's outermost lifetime boundary, which bounds work
// that was deliberately detached from its creator's scope.
func (state *State) Root() *lifetime.Scope {
	return state.root
}

// WithScope derives state whose nested work is owned by scope.
func (state *State) WithScope(scope *lifetime.Scope) *State {
	derived := *state
	derived.scope = scope
	return &derived
}

// Metadata returns a defensive snapshot of current observation metadata.
func (state *State) Metadata() Metadata {
	metadata := state.metadata
	metadata.Attributes = slices.Clone(metadata.Attributes)
	return metadata
}

// Named derives state with a user-facing operation name.
func (state *State) Named(name string) *State {
	derived := *state
	derived.metadata.Operation = name
	return &derived
}

// Annotated derives state with ordered structured attributes.
func (state *State) Annotated(attributes []slog.Attr) *State {
	derived := *state
	derived.metadata.Attributes = append(slices.Clone(state.metadata.Attributes), attributes...)
	return &derived
}

// SpanBoundary describes one named observation boundary. Grouping its parts
// keeps Spanned's signature narrow as the boundary gains metadata.
type SpanBoundary struct {
	Name       string
	Source     string
	Attributes []slog.Attr
}

// Spanned derives state with a fresh runtime-local span identity.
func (state *State) Spanned(boundary SpanBoundary) *State {
	derived := *state
	derived.metadata.ParentID = state.metadata.SpanID
	derived.metadata.SpanID = state.identifiers.spans.Add(1)
	derived.metadata.Operation = boundary.Name
	derived.metadata.Source = boundary.Source
	derived.metadata.Attributes = append(slices.Clone(state.metadata.Attributes), boundary.Attributes...)
	return &derived
}

// Forked derives state for a child fiber: it inherits names, annotations and
// span identity, and records the identity reserved for it plus its parent's.
func (state *State) Forked(scope *lifetime.Scope, fiberID uint64) *State {
	derived := *state
	derived.scope = scope
	derived.metadata.ParentFiber = state.metadata.FiberID
	derived.metadata.FiberID = fiberID
	derived.metadata.Attributes = slices.Clone(state.metadata.Attributes)
	return &derived
}

// NextFiberID reserves the next runtime-local fiber identity.
func (state *State) NextFiberID() uint64 {
	return state.identifiers.fibers.Add(1)
}

// Observing reports whether event construction is necessary.
func (state *State) Observing() bool {
	return state.capabilities.Observer != nil
}

// Event constructs an event from inherited metadata and the runtime clock.
func (state *State) Event(kind capability.EventKind) capability.RuntimeEvent {
	return capability.RuntimeEvent{
		Kind:        kind,
		Timestamp:   state.capabilities.Clock.Now(),
		Operation:   state.metadata.Operation,
		Source:      state.metadata.Source,
		FiberID:     state.metadata.FiberID,
		ParentFiber: state.metadata.ParentFiber,
		SpanID:      state.metadata.SpanID,
		ParentID:    state.metadata.ParentID,
		Attributes:  slices.Clone(state.metadata.Attributes),
	}
}

// Emit delivers an event, containing observer panics so instrumentation can
// never change an application's Exit. A contained panic is reported to
// diagnostics rather than discarded.
func (state *State) Emit(ctx context.Context, event capability.RuntimeEvent) {
	observer := state.capabilities.Observer
	if observer == nil {
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			state.Report(ctx, capability.RuntimeFault{
				Component: capability.FaultObserver,
				Operation: string(event.Kind),
				Err:       fmt.Errorf("effect: observer panicked: %v", recovered),
			})
		}
	}()
	observer.Observe(ctx, event)
}

// Report routes an instrumentation fault to the runtime's diagnostics sink.
func (state *State) Report(ctx context.Context, fault capability.RuntimeFault) {
	diagnostics := state.capabilities.Diagnostics
	if diagnostics == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	diagnostics.Report(ctx, fault)
}
