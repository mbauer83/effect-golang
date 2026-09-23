package runtime

import (
	"context"
	"fmt"
	"github.com/mbauer83/effect-golang/effect/internal/lifetime"
	"github.com/mbauer83/effect-golang/effect/internal/outcome"
)

// Branch is one unit of concurrent work in a collection composition.
type Branch func(context.Context, *State) outcome.Exit

// RunAll evaluates every branch concurrently inside a private scope and
// returns their terminal exits in input order.
//
// At most limit branches run at once; a limit of zero or less is unbounded.
// Bounding uses a buffered channel as a semaphore rather than a worker pool, so
// each branch remains one ordinary goroutine owned by the private scope and the
// Go scheduler still decides when it runs.
//
// As soon as any branch fails, the rest are canceled -- their results can no
// longer be needed -- but RunAll still waits for every branch and for the
// private scope's finalizers before returning.
func RunAll(
	interpretation Interpretation,
	branches []Branch,
	limit int,
	reason error,
) ([]outcome.Exit, outcome.Cause) {
	if len(branches) == 0 {
		return nil, outcome.Cause{}
	}

	scope := lifetime.NewScope(interpretation.Context)
	group, unavailable := startAll(scope, interpretation.State, branches, limit)
	if unavailable != nil {
		return nil, *unavailable
	}

	group.awaitFirstFailure(scope, reason)
	exits := group.collect()
	return exits, scope.Close(interpretation.Context, outcome.Success(exits), reason)
}

// completion reports a branch's own exit as soon as its body finishes, before
// its child scope has finalized. That is early enough to decide whether the
// remaining branches are still needed, and the authoritative exit is read from
// the fiber afterwards.
type completion struct {
	index int
	exit  outcome.Exit
}

type branchGroup struct {
	fibers      []*lifetime.Fiber
	completions chan completion
}

func startAll(scope *lifetime.Scope, state *State, branches []Branch, limit int) (*branchGroup, *outcome.Cause) {
	group := &branchGroup{
		fibers:      make([]*lifetime.Fiber, len(branches)),
		completions: make(chan completion, len(branches)),
	}
	permits := permitsFor(limit, len(branches))

	for index, work := range branches {
		fiber, started := StartFiber(scope, scope.Context(), state,
			group.recordOutcome(index, withPermit(permits, work)),
		)
		if !started {
			failure := outcome.DieCause(outcome.Defect{
				Value: fmt.Errorf("effect: fresh scope rejected parallel branch %d", index),
			})
			return nil, &failure
		}
		group.fibers[index] = fiber
	}
	return group, nil
}

func permitsFor(limit int, total int) chan struct{} {
	if limit <= 0 || limit >= total {
		return nil
	}
	return make(chan struct{}, limit)
}

// withPermit makes a branch wait for a permit before it runs. Waiting selects on the
// branch's own context, so a bounded traversal stays cancelable while queued.
func withPermit(permits chan struct{}, work Branch) Branch {
	if permits == nil {
		return work
	}
	return func(ctx context.Context, state *State) outcome.Exit {
		select {
		case permits <- struct{}{}:
			defer func() { <-permits }()
		case <-ctx.Done():
			return outcome.Failure(outcome.InterruptCause(lifetime.CancellationReason(ctx)))
		}
		return work(ctx, state)
	}
}

func (group *branchGroup) recordOutcome(index int, work Branch) Branch {
	return func(ctx context.Context, state *State) outcome.Exit {
		exit := work(ctx, state)
		group.completions <- completion{index: index, exit: exit}
		return exit
	}
}

// awaitFirstFailure watches branch completions and cancels the scope once one
// branch has failed, so the remaining work stops promptly.
func (group *branchGroup) awaitFirstFailure(scope *lifetime.Scope, reason error) {
	abandoned := false
	for range group.fibers {
		reported := <-group.completions
		if reported.exit.IsSuccess() || abandoned {
			continue
		}
		abandoned = true
		scope.Interrupt(reason)
	}
}

func (group *branchGroup) collect() []outcome.Exit {
	exits := make([]outcome.Exit, len(group.fibers))
	for index, fiber := range group.fibers {
		exits[index] = fiber.Wait()
	}
	return exits
}
