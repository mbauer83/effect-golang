package rate

// Waiting for a turn.

import (
	"context"
	"errors"
	"time"

	"github.com/mbauer83/effect-golang/effect"
)

// Refusals a limiter answers with.
var (
	// ErrUnstated is an allowance that says nothing about what is allowed,
	// which is refused rather than treated as unlimited: a service asked at
	// an unstated rate is one that eventually blocks the program asking.
	ErrUnstated = errors.New("an allowance is a count and a period, under a name")
	// ErrLimitExceeded is a turn further off than the caller said it would wait for.
	ErrLimitExceeded = errors.New("the queue is longer than this program will wait")
)

// AwaitTurn is this program's turn, waited for.
//
// The whole of what a caller wants: reserve a moment, and be interpreted at
// it. The limiter refuses rather than reserving when the queue is longer than
// the caller will wait, because a request that would wait four minutes for
// its turn is one whose caller has long since gone -- and a refusal somebody
// can be shown beats a page that never arrives.
//
// Which makes longest more than a patience: it is how much of somebody else's
// allowance this caller is entitled to queue for. Work that states a small
// one takes only the room that happens to be free and never pushes back work
// that states a large one, so speculative reading cannot starve the reading
// somebody is waiting on. The two are the same allowance, counted once, and
// no priority scheme is needed to keep them apart.
func AwaitTurn[R any](
	limiter Limiter,
	allowance Allowance,
	longest time.Duration,
) effect.Effect[R, Fault, effect.Unit] {
	if !allowance.IsStated() {
		return effect.Fail[R, effect.Unit](Fault{Allowance: allowance.Name, Err: ErrUnstated})
	}
	return reserveTurn[R](limiter, allowance, longest).
		FlatMap(func(wait time.Duration) effect.Effect[R, Fault, effect.Unit] {
			if wait <= 0 {
				return effect.Succeed[R, Fault](effect.Unit{})
			}
			return effect.Sleep[R, Fault](wait)
		}).
		WithName("rate wait")
}

// reserveTurn is the turn itself, without the waiting.
//
// Exported nowhere, because a caller that took a turn and did not wait for it
// would have spent an allowance it then exceeded. Waiting is the only way to
// take one.
func reserveTurn[R any](
	limiter Limiter,
	allowance Allowance,
	longest time.Duration,
) effect.Effect[R, Fault, time.Duration] {
	return effect.Try(
		func(ctx context.Context, _ R) (time.Duration, error) {
			return limiter.Turn(ctx, allowance, longest)
		},
		func(err error) Fault {
			var existing Fault
			if errors.As(err, &existing) {
				return existing
			}
			return Fault{Allowance: allowance.Name, Err: err}
		},
	)
}
