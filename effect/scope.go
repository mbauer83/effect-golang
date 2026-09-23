package effect

import (
	"context"
	"time"

	"github.com/mbauer83/effect-golang/effect/capability"
	"github.com/mbauer83/effect-golang/effect/internal/lifetime"
	"github.com/mbauer83/effect-golang/effect/internal/outcome"
	runtimecore "github.com/mbauer83/effect-golang/effect/internal/runtime"
)

// Scope is a handle to one lifetime boundary: the fibers started inside it and
// the resources acquired inside it. It is cheap to copy, because its
// synchronization state stays behind a pointer and is never copied.
//
// A Scope exposes no Close. The code that created the scope owns its closure,
// so no component can destroy a lifetime boundary that belongs to someone else.
type Scope struct {
	state *lifetime.Scope
}

// Scoped creates a lexical lifetime boundary and hands it to use.
//
// Everything use acquires through the scope is released, and everything it
// forks into the scope is awaited, before the returned effect completes --
// whether it succeeded, failed, defected or was interrupted. Release failures
// are appended to the outcome's cause with Then rather than replacing it.
//
// Scope is a lexical parameter rather than part of R. The structural Product
// environment cannot subtract a requirement once it has been composed, so
// putting a scope there would make every resource-using effect's public type
// unusable.
func Scoped[R, E, A any](use func(Scope) Effect[R, E, A]) Effect[R, E, A] {
	return suspendRuntime(func(ctx context.Context, state *runtimecore.State, _ R) Effect[R, E, A] {
		scope := lifetime.NewScope(ctx)
		openedAt := state.EmitStart(ctx, capability.EventScopeOpened)

		// use runs inside the subtree that already carries the closing hook, so
		// a panic while describing the body still closes the scope.
		body := suspendRuntime(func(context.Context, *runtimecore.State, R) Effect[R, E, A] {
			return use(Scope{state: scope})
		})
		return body.
			withExitObserver(closeScope(scope, openedAt)).
			withState(replaceState(state.WithScope(scope))).
			withContext(scope.Context())
	})
}

// closeScope ends the scope's lifetime once the body it owns has settled, and
// composes any release failure after the body's own cause.
func closeScope(scope *lifetime.Scope, openedAt time.Time) exitObserver {
	return func(interpretation runtimecore.Interpretation, exit outcome.Exit) outcome.Exit {
		ctx, state := interpretation.Context, interpretation.State
		state.EmitMark(ctx, capability.EventScopeClosing)
		cleanup := scope.Close(ctx, exit, lifetime.ErrScopeClosed)
		state.EmitEnd(
			ctx,
			capability.EventScopeClosed,
			openedAt,
			outcome.CleanupStatus(cleanup),
		)

		if cleanup.IsEmpty() {
			return exit
		}
		return outcome.Failure(exit.Cause().Then(cleanup))
	}
}

// AcquireRelease acquires a resource and registers its release in this scope.
//
// Registration is atomic with acquisition: once acquire has produced a
// resource the scope owns its release before the runtime reaches another
// interruption checkpoint, so a cancellation arriving at that moment cannot
// leak it. If acquisition is itself interrupted before producing a resource,
// cleanup of any partial state belongs to the acquisition, which is the
// conventional contract of a context-aware Go API.
//
// Release runs exactly once, in reverse acquisition order, with a context
// detached from cancellation so an already-canceled caller cannot skip it.
//
// The release workflow has a Never failure channel because one scope may hold
// unrelated resources whose release errors share no type, and those types
// cannot be added to E after the effects have been composed. Use Release to
// turn a conventional Close error into a defect.
func (scope Scope) AcquireRelease[R, E, A any](
	acquire Effect[R, E, A],
	release func(A) Effect[R, Never, Unit],
) Effect[R, E, A] {
	return acquire.withExitObserver(registerRelease(scope.state, release))
}

// AddFinalizer adapts a conventional Go release function into an infallible release
// workflow. A non-nil error becomes a defect, which the closing cause preserves
// rather than silently discarding.
//
// Which puts a judgement on the caller, and it is the one most easily missed:
// a release runs after the work it belongs to has finished, and a fiber's
// context is cancelled when the fiber finishes -- so for any work that failed
// or was interrupted, the release runs with a context that is already done.
// Drivers notice. A rollback answers that the context is done, a cursor's
// close answers "context canceled", and a release that reported those as
// errors put a defect beside every typed refusal its work raised. A cause
// carrying a defect is what a boundary answers as a five hundred, so the
// refusal a caller was meant to act on never reached them.
//
// Such an error is usually the outcome the release wanted rather than its
// failure: a cancelled context takes the connection with it, so what needed
// releasing was released before anything asked. Usually and not always, which
// is why this does not filter them here -- a release that had to reach the
// network to let go and could not is a real leak, and only the caller knows
// which of the two it is holding. So: decide, and say which in the release.
//
//	effect.AddFinalizer[R](func(context.Context) error {
//	    if err := held.Close(); err != nil && !alreadyGone(err) {
//	        return err
//	    }
//	    return nil
//	})
func AddFinalizer[R any](release func(context.Context) error) Effect[R, Never, Unit] {
	return From(func(ctx context.Context, _ R) Exit[Never, Unit] {
		if err := release(ctx); err != nil {
			return ExitCause[Never, Unit](DieCause[Never](Defect{Value: err}))
		}
		return ExitSuccess[Never](Unit{})
	})
}

func registerRelease[R, A any](
	scope *lifetime.Scope,
	release func(A) Effect[R, Never, Unit],
) exitObserver {
	return func(interpretation runtimecore.Interpretation, exit outcome.Exit) outcome.Exit {
		if !exit.IsSuccess() {
			return exit
		}

		resource := asValue[A](exit.Value())
		accepted, cleanup := scope.AddFinalizer(
			interpretation.Context,
			releaseFinalizer(interpretation, release, resource),
		)
		if accepted {
			interpretation.State.Ledger().RecordAcquisition()
			interpretation.State.EmitMark(interpretation.Context, capability.EventResourceAcquired)
			return exit
		}

		// The scope had already begun closing, so the resource was released
		// immediately. Reporting the closed lifetime keeps the caller from
		// using a resource that no longer exists.
		return outcome.Failure(
			outcome.InterruptCause(lifetime.ErrScopeClosed).Then(cleanup),
		)
	}
}

func releaseFinalizer[R, A any](
	interpretation runtimecore.Interpretation,
	release func(A) Effect[R, Never, Unit],
	resource A,
) lifetime.Finalizer {
	environment := asEnvironment[R](interpretation.Environment)
	state := interpretation.State
	return func(ctx context.Context, _ outcome.Exit) outcome.Cause {
		released := release(resource).run(ctx, state, environment)
		state.Ledger().RecordRelease()
		state.EmitMark(ctx, capability.EventResourceReleased)
		cause, _ := released.Cause()
		return cause.node
	}
}
