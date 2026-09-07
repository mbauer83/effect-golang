package effect

import (
	"context"

	"github.com/mbauer83/effect-golang/effect/internal/lifetime"
	runtimecore "github.com/mbauer83/effect-golang/effect/internal/runtime"
)

// Fork starts fx on its own goroutine, owned by the current dynamic scope, and
// immediately returns a Fiber observing it.
//
// The child cannot become an orphan: the enclosing scope waits for it before
// releasing the resources that scope owns, so a fork inside Scoped can never
// outlive the resources it may be using. Forking into a scope that has already
// begun closing fails with an interruption naming ErrScopeClosed rather than
// starting unowned work.
//
// It is a package function because its success channel is built from fx's own
// channels (golang/go#80172).
func Fork[R, E, A any](fx Effect[R, E, A]) Effect[R, Never, Fiber[E, A]] {
	return forking(fx, currentScope)
}

// Fork starts fx owned by this scope rather than by the current dynamic one,
// for the advanced case where a child must deliberately have a different
// lifetime from its creator.
func (scope Scope) Fork[R, E, A any](fx Effect[R, E, A]) Effect[R, Never, Fiber[E, A]] {
	return forking(fx, func(*runtimecore.State) *lifetime.Scope {
		return scope.state
	})
}

// ForkDaemon starts fx owned by the Runtime's root scope, so it outlives the
// Run that created it. Detached work is still owned: Runtime.Close interrupts
// and awaits it.
func ForkDaemon[R, E, A any](fx Effect[R, E, A]) Effect[R, Never, Fiber[E, A]] {
	return forking(fx, (*runtimecore.State).Root)
}

// scopeSelector chooses which lifetime owns a newly forked fiber.
type scopeSelector func(*runtimecore.State) *lifetime.Scope

func currentScope(state *runtimecore.State) *lifetime.Scope {
	return state.Scope()
}

func forking[R, E, A any](fx Effect[R, E, A], selectOwner scopeSelector) Effect[R, Never, Fiber[E, A]] {
	return fromRuntime(func(
		ctx context.Context,
		state *runtimecore.State,
		env R,
	) Exit[Never, Fiber[E, A]] {
		started, accepted := runtimecore.StartFiber(selectOwner(state), ctx, state,
			erasedWork(fx, env),
		)
		if !accepted {
			return exitInterrupted[Never, Fiber[E, A]](lifetime.ErrScopeClosed)
		}
		return ExitSuccess[Never](Fiber[E, A]{state: started})
	})
}
