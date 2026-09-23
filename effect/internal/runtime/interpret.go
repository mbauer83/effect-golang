package runtime

import (
	"context"
	"fmt"
	"github.com/mbauer83/effect-golang/effect/internal/lifetime"
	"github.com/mbauer83/effect-golang/effect/internal/outcome"
)

// Interpret evaluates an erased instruction tree iteratively.
//
// Sequential composition consumes heap-allocated continuation frames instead of
// Go stack frames, so an arbitrarily deep program neither overflows nor grows
// the goroutine stack. No goroutine is created and no scheduling decision is
// made here: the Go runtime remains the only scheduler.
func Interpret(ctx context.Context, state *State, environment any, node Node) outcome.Exit {
	machine := interpreter{ctx: ctx, state: state, environment: environment}
	return machine.run(node)
}

type interpreter struct {
	ctx         context.Context
	state       *State
	environment any
	frames      []frame
}

type frame interface {
	continuation()
}

type transformFrame struct{ apply func(any) any }
type bindFrame struct{ continueWith func(any) Node }
type transformCauseFrame struct {
	apply func(outcome.Cause) outcome.Cause
}
type recoverFrame struct{ handle func(outcome.Cause) Node }
type environmentFrame struct{ environment any }
type stateFrame struct{ state *State }
type contextFrame struct{ ctx context.Context }
type exitHookFrame struct {
	observe func(Interpretation, outcome.Exit) outcome.Exit
}

func (transformFrame) continuation()      {}
func (bindFrame) continuation()           {}
func (transformCauseFrame) continuation() {}
func (recoverFrame) continuation()        {}
func (environmentFrame) continuation()    {}
func (stateFrame) continuation()          {}
func (contextFrame) continuation()        {}
func (exitHookFrame) continuation()       {}

func (machine *interpreter) run(node Node) outcome.Exit {
	current := node
	for {
		exit := machine.evaluate(current)
		resumed, settled, pending := machine.resume(exit)
		if !pending {
			return settled
		}
		current = resumed
	}
}

// evaluate descends through composition instructions, recording one
// continuation frame per level, until it reaches an instruction that settles.
func (machine *interpreter) evaluate(node Node) outcome.Exit {
	for {
		switch instruction := node.(type) {
		case *Transform:
			machine.push(transformFrame{apply: instruction.Apply})
			node = instruction.Source
		case *Bind:
			machine.push(bindFrame{continueWith: instruction.Continue})
			node = instruction.Source
		case *TransformCause:
			machine.push(transformCauseFrame{apply: instruction.Apply})
			node = instruction.Source
		case *Recover:
			machine.push(recoverFrame{handle: instruction.Handle})
			node = instruction.Source
		case *OnExit:
			machine.push(exitHookFrame{observe: instruction.Observe})
			node = instruction.Source
		case *WithEnvironment:
			machine.push(environmentFrame{environment: machine.environment})
			adapted, defect := adaptEnvironment(instruction.Adapt, machine.environment)
			if defect != nil {
				return outcome.Failure(outcome.DieCause(*defect))
			}
			machine.environment = adapted
			node = instruction.Source
		case *WithContext:
			machine.push(contextFrame{ctx: machine.ctx})
			derived, defect := deriveContext(instruction.Derive, machine.ctx)
			if defect != nil {
				return outcome.Failure(outcome.DieCause(*defect))
			}
			machine.ctx = derived
			node = instruction.Source
		case *WithState:
			machine.push(stateFrame{state: machine.state})
			derived, defect := deriveState(instruction.Derive, machine.state)
			if defect != nil {
				return outcome.Failure(outcome.DieCause(*defect))
			}
			machine.state = derived
			node = instruction.Source
		case *Suspend:
			created, defect := createNode(instruction, machine.interpretation())
			if defect != nil {
				return outcome.Failure(outcome.DieCause(*defect))
			}
			node = created
		default:
			return machine.settle(node)
		}
	}
}

// settle evaluates an instruction that performs no further composition. Every
// value a program produces passes through exactly one settling instruction, so
// this is also the runtime's cooperative interruption checkpoint.
func (machine *interpreter) settle(node Node) outcome.Exit {
	if cause, interrupted := machine.interruptCause(); interrupted {
		return outcome.Failure(cause)
	}
	switch instruction := node.(type) {
	case *Succeed:
		return outcome.Success(instruction.Value)
	case *Fail:
		// Stamped here because only the interpreter knows what was going on:
		// a failure records its line where it is written and the span it was
		// inside where it is run, and the second is what says which request
		// or which stage rather than which line.
		return outcome.Failure(instruction.Cause.WithOrigin(outcome.Origin{
			Operation: machine.state.Metadata().Operation,
		}))
	case *Eval:
		return evalLeaf(instruction, machine.interpretation())
	default:
		return outcome.Failure(outcome.DieCause(outcome.Defect{Value: fmt.Errorf("effect: unsupported instruction %T", node)}))
	}
}

// resume applies pending continuations to exit until one of them produces
// another instruction to evaluate.
func (machine *interpreter) resume(exit outcome.Exit) (Node, outcome.Exit, bool) {
	for len(machine.frames) > 0 {
		switch continuation := machine.pop().(type) {
		case environmentFrame:
			machine.environment = continuation.environment
		case stateFrame:
			machine.state = continuation.state
		case contextFrame:
			machine.ctx = continuation.ctx
		case transformFrame:
			if exit.IsSuccess() {
				exit = transformExit(continuation.apply, exit.Value())
			}
		case transformCauseFrame:
			if !exit.IsSuccess() {
				exit = transformCause(continuation.apply, exit.Cause())
			}
		case exitHookFrame:
			exit = observeExit(continuation.observe, machine.interpretation(), exit)
		case bindFrame:
			if !exit.IsSuccess() {
				continue
			}
			node, defect := continueNode(continuation.continueWith, exit.Value())
			if defect != nil {
				exit = outcome.Failure(outcome.DieCause(*defect))
				continue
			}
			return node, exit, true
		case recoverFrame:
			if exit.IsSuccess() {
				continue
			}
			node, defect := handleCause(continuation.handle, exit.Cause())
			if defect != nil {
				exit = outcome.Failure(exit.Cause().Then(outcome.DieCause(*defect)))
				continue
			}
			return node, exit, true
		}
	}
	return nil, exit, false
}

// interpretation snapshots the ambient state for one instruction callback.
func (machine *interpreter) interpretation() Interpretation {
	return Interpretation{
		Context:     machine.ctx,
		State:       machine.state,
		Environment: machine.environment,
	}
}

func (machine *interpreter) push(continuation frame) {
	machine.frames = append(machine.frames, continuation)
}

func (machine *interpreter) pop() frame {
	last := len(machine.frames) - 1
	continuation := machine.frames[last]
	machine.frames = machine.frames[:last]
	return continuation
}

func (machine *interpreter) interruptCause() (outcome.Cause, bool) {
	if machine.ctx.Err() == nil {
		return outcome.Cause{}, false
	}
	return outcome.InterruptCause(lifetime.CancellationReason(machine.ctx)), true
}
