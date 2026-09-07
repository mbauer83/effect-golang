package runtime

import (
	"context"
	"github.com/mbauer83/effect-golang/internal/outcome"
)

// Interpretation is the ambient state an instruction runs with. Passing it as
// one value keeps instruction callbacks narrow and makes it explicit that the
// context, the runtime services and the environment always travel together.
type Interpretation struct {
	Context     context.Context
	State       *State
	Environment any
}

// Node is one instruction of an erased effect program.
//
// The typed public API is the only constructor of these instructions, so every
// erased callback below receives exactly the dynamic types its effect's R, E
// and A channels promise. The interpreter therefore never inspects or asserts
// an erased value; it only routes it back to typed code.
type Node interface {
	instruction()
}

// Succeed produces a value without performing work.
type Succeed struct {
	Value any
}

// Fail terminates with a complete cause.
type Fail struct {
	Cause outcome.Cause
}

// Eval is an opaque leaf that performs work.
type Eval struct {
	Run func(Interpretation) outcome.Exit
}

// Suspend defers instruction construction until interpretation.
type Suspend struct {
	Create func(Interpretation) Node
}

// Transform rewrites a successful value.
type Transform struct {
	Source Node
	Apply  func(any) any
}

// Bind continues with another instruction derived from a successful value.
type Bind struct {
	Source   Node
	Continue func(any) Node
}

// TransformCause rewrites an unsuccessful cause.
type TransformCause struct {
	Source Node
	Apply  func(outcome.Cause) outcome.Cause
}

// Recover continues with another instruction derived from a cause.
type Recover struct {
	Source Node
	Handle func(outcome.Cause) Node
}

// WithEnvironment adapts the environment supplied to Source.
type WithEnvironment struct {
	Source Node
	Adapt  func(any) any
}

// WithState derives the runtime state supplied to Source.
type WithState struct {
	Source Node
	Derive func(*State) *State
}

// WithContext derives the context supplied to Source. It is how a scope gives
// the work it owns a cancelable lifetime of its own.
type WithContext struct {
	Source Node
	Derive func(context.Context) context.Context
}

// OnExit observes and may rewrite Source's exit, including on failure,
// interruption and defect. Unlike Recover it runs outside the interruption
// checkpoint, so a registration or cleanup step cannot be skipped by a
// cancellation that arrived while Source was running.
type OnExit struct {
	Source  Node
	Observe func(Interpretation, outcome.Exit) outcome.Exit
}

func (*Succeed) instruction()         {}
func (*Fail) instruction()            {}
func (*Eval) instruction()            {}
func (*Suspend) instruction()         {}
func (*Transform) instruction()       {}
func (*Bind) instruction()            {}
func (*TransformCause) instruction()  {}
func (*Recover) instruction()         {}
func (*WithEnvironment) instruction() {}
func (*WithState) instruction()       {}
func (*WithContext) instruction()     {}
func (*OnExit) instruction()          {}
