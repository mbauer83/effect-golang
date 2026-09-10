// Package direct is an experimental direct-style alternative to Workflow.
//
// It lets a dependent sequence read as ordinary Go:
//
//	program := direct.Run(func(bind *direct.Binder[Env, AppError]) Quote {
//	    customer := direct.Bind(bind, loadCustomer(id))
//	    basket := direct.Bind(bind, loadBasket(customer))
//	    return price(customer, basket)
//	})
//
// # Why this is experimental
//
// Bind must return an A while also abandoning the callback when the effect did
// not succeed. Go has no resumable suspension and no typed early-return
// protocol, so the only way to do that is a panic. The sentinel is private and
// carries a token unique to one Run, so a nested Run cannot catch an outer
// one's short-circuit, but three costs are real and unfixable:
//
//   - a deferred block in the body runs on every expected failure, not only on
//     an exceptional one;
//   - a broad recover() in the body can swallow the short-circuit. That is
//     detected and reported as a defect rather than silently returning a value
//     the program never computed, but it cannot be prevented;
//   - the cost of a panic is paid on the expected-failure path.
//
// Everything else behaves exactly as the core operators do, because it is built
// on them: the effect stays lazy, cancellation is observed, a user panic still
// becomes a defect, and the runtime's capabilities and scope are the ones the
// surrounding interpretation is using.
//
// Prefer Workflow. Reach for this only when the explicit state type is the
// thing making a workflow hard to read, and read
// docs/explanation/sequencing-in-go.md first.
package direct

import (
	"fmt"
	"runtime/debug"

	"github.com/mbauer83/effect-golang/effect"
)

// Binder short-circuits the enclosing Run when a bound effect does not succeed.
//
// It is valid only while the body that received it is running, and only on the
// goroutine running that body. Publishing it does not work and says so.
type Binder[R, E any] struct {
	interpreter effect.Interpreter[R, E]
	progress    *progress[E]
}

// progress is one Run's short-circuit state.
//
// token identifies this Run's sentinel, so a nested Run recovers only its own.
// tripped records that a short-circuit was raised, which is how a sentinel a
// recover() in the body swallowed is detected afterwards.
type progress[E any] struct {
	token   *sentinel
	cause   effect.Cause[E]
	tripped bool
	live    bool
}

// sentinel is the private panic value. Its type is unexported, so nothing
// outside this package can construct one or match on it.
type sentinel struct{}

// Run interprets body in direct style.
func Run[R, E, A any](body func(*Binder[R, E]) A) effect.Effect[R, E, A] {
	return effect.WithInterpreter(func(interpreter effect.Interpreter[R, E]) effect.Exit[E, A] {
		binder := &Binder[R, E]{
			interpreter: interpreter,
			progress:    &progress[E]{token: &sentinel{}, live: true},
		}
		return evaluateBody(binder, body)
	})
}

// Bind evaluates fx and returns its value, abandoning the body when it did not
// succeed. A typed failure, a defect and an interruption all abandon it, and
// all reach the resulting effect unchanged.
//
// It is a package function because its result is the effect's own success type
// and its receiver is generic (golang/go#80172).
func Bind[R, E, A any](binder *Binder[R, E], fx effect.Effect[R, E, A]) A {
	if binder == nil || binder.progress == nil || !binder.progress.live {
		panic(fmt.Errorf("direct: Binder used outside the Run body that created it"))
	}

	exit := effect.Interpret(binder.interpreter, fx)
	if value, ok := exit.Value(); ok {
		return value
	}

	cause, _ := exit.Cause()
	binder.progress.cause = cause
	binder.progress.tripped = true
	panic(binder.progress.token)
}

// evaluateBody runs the body and turns its three possible endings into an Exit:
// a normal return, this Run's short-circuit, or a panic.
func evaluateBody[R, E, A any](binder *Binder[R, E], body func(*Binder[R, E]) A) (exit effect.Exit[E, A]) {
	defer func() {
		binder.progress.live = false
		exit = settle(binder.progress, exit, recover())
	}()
	return effect.ExitSuccess[E](body(binder))
}

func settle[E, A any](state *progress[E], returned effect.Exit[E, A], recovered any) effect.Exit[E, A] {
	switch {
	case recovered == state.token:
		return effect.ExitCause[E, A](state.cause)
	case recovered != nil:
		// A panic from the body, or a nested Run's sentinel escaping because
		// something recovered ours. Either way it is a defect, captured here
		// rather than re-panicked so the stack is not unwound twice.
		return effect.ExitCause[E, A](effect.DieCause[E](effect.Defect{
			Value: recovered,
			Stack: string(debug.Stack()),
		}))
	case state.tripped:
		// The body returned a value although a Bind had short-circuited, which
		// means a recover() in the body swallowed the sentinel. Returning that
		// value would report a result the program never computed.
		return effect.ExitCause[E, A](effect.DieCause[E](effect.Defect{
			Value: fmt.Errorf(
				"direct: a recover() in the Run body swallowed a Bind short-circuit; "+
					"the abandoned cause was %v", state.cause),
			Stack: string(debug.Stack()),
		}))
	default:
		return returned
	}
}

// Fail abandons the body with this failure.
//
// A guard clause. Every judgement a step makes has this shape -- the aggregate
// refused, so there is nothing to write and nothing to answer with -- and
// without it the refusal has to be bound as an effect whose success type is
// the one the body returns:
//
//	return Bind(bind, effect.Fail[Services, Tracking](refused))   // before
//	Fail(bind, refused)                                           // after
//
// The difference is not the line count. The first names a type the failure
// does not have, which reads as though the failing branch produced a tracking,
// and it can only appear where the body returns rather than where the
// judgement was made.
//
// It does not return: like a bound failure, it unwinds to the enclosing Run.
func Fail[R, E any](binder *Binder[R, E], failure E) {
	Bind(binder, effect.Fail[R, never](failure))
}

// never is the success type of an effect that has none. Unexported and
// uninhabited, so the only thing Fail can do is fail.
type never struct{ _ [0]func() }
