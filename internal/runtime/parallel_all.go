package runtime

import (
	"context"
	"fmt"
)

// Branch is one unit of concurrent work in a collection composition.
type Branch func(context.Context, *State) Exit

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
) ([]Exit, Cause) {
	if len(branches) == 0 {
		return nil, Cause{}
	}

	scope := NewScope(interpretation.Context)
	group, unavailable := startAll(scope, interpretation.State, branches, limit)
	if unavailable != nil {
		return nil, *unavailable
	}

	group.awaitFirstFailure(scope, reason)
	exits := group.collect()
	return exits, scope.Close(interpretation.Context, Success(exits), reason)
}

// completion reports a branch's own exit as soon as its body finishes, before
// its child scope has finalized. That is early enough to decide whether the
// remaining branches are still needed, and the authoritative exit is read from
// the fiber afterwards.
type completion struct {
	index int
	exit  Exit
}

type branchGroup struct {
	fibers      []*Fiber
	completions chan completion
}

func startAll(scope *Scope, state *State, branches []Branch, limit int) (*branchGroup, *Cause) {
	group := &branchGroup{
		fibers:      make([]*Fiber, len(branches)),
		completions: make(chan completion, len(branches)),
	}
	permits := permitsFor(limit, len(branches))

	for index, work := range branches {
		fiber, started := StartFiber(scope, scope.Context(), state,
			group.reporting(index, gated(permits, work)),
		)
		if !started {
			failure := DieCause(Defect{
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

// gated makes a branch wait for a permit before it runs. Waiting selects on the
// branch's own context, so a bounded traversal stays cancelable while queued.
func gated(permits chan struct{}, work Branch) Branch {
	if permits == nil {
		return work
	}
	return func(ctx context.Context, state *State) Exit {
		select {
		case permits <- struct{}{}:
			defer func() { <-permits }()
		case <-ctx.Done():
			return Failure(InterruptCause(CancellationReason(ctx)))
		}
		return work(ctx, state)
	}
}

func (group *branchGroup) reporting(index int, work Branch) Branch {
	return func(ctx context.Context, state *State) Exit {
		exit := work(ctx, state)
		group.completions <- completion{index: index, exit: exit}
		return exit
	}
}

// awaitFirstFailure watches branch completions and cancels the scope once one
// branch has failed, so the remaining work stops promptly.
func (group *branchGroup) awaitFirstFailure(scope *Scope, reason error) {
	abandoned := false
	for range group.fibers {
		reported := <-group.completions
		if reported.exit.Succeeded() || abandoned {
			continue
		}
		abandoned = true
		scope.Interrupt(reason)
	}
}

func (group *branchGroup) collect() []Exit {
	exits := make([]Exit, len(group.fibers))
	for index, fiber := range group.fibers {
		exits[index] = fiber.Wait()
	}
	return exits
}

// CombineBranchCauses composes the causes of a collection composition in input
// order. A branch canceled only because a sibling failed did not fail on its
// own account, so its induced interruption is dropped.
func CombineBranchCauses(exits []Exit, induced error) Cause {
	combined := Cause{}
	for _, exit := range exits {
		cause := exit.Cause()
		if WasInduced(cause, induced) {
			continue
		}
		combined = combined.Both(cause)
	}
	return combined
}
