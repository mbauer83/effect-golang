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
	// ErrQueued is a turn further off than the caller said it would wait for.
	ErrQueued = errors.New("the queue is longer than this program will wait")
)

// Waiting is this program's turn, waited for.
//
// The whole of what a caller wants: reserve a moment, and be interpreted at
// it. Failing rather than sleeping when the queue is longer than the caller
// will wait, because a request that would wait four minutes for its turn is
// one whose caller has long since gone, and a refusal somebody can be shown
// beats a page that never arrives.
func Waiting[R any](
	limiter Limiter,
	allowance Allowance,
	longest time.Duration,
) effect.Effect[R, Fault, effect.Unit] {
	if !allowance.IsStated() {
		return effect.Fail[R, effect.Unit](Fault{Allowance: allowance.Name, Err: ErrUnstated})
	}
	return reserving[R](limiter, allowance).
		FlatMap(func(wait time.Duration) effect.Effect[R, Fault, effect.Unit] {
			switch {
			case wait <= 0:
				return effect.Succeed[R, Fault](effect.Unit{})
			case longest > 0 && wait > longest:
				return effect.Fail[R, effect.Unit](Fault{
					Allowance: allowance.Name, Err: ErrQueued,
				})
			default:
				return effect.Sleep[R, Fault](wait)
			}
		}).
		Named("rate wait")
}

// reserving is the turn itself, without the waiting.
//
// Exported nowhere, because a caller that took a turn and did not wait for it
// would have spent an allowance it then exceeded. Waiting is the only way to
// take one.
func reserving[R any](limiter Limiter, allowance Allowance) effect.Effect[R, Fault, time.Duration] {
	return effect.Try(
		func(ctx context.Context, _ R) (time.Duration, error) {
			return limiter.Turn(ctx, allowance)
		},
		func(err error) Fault {
			var already Fault
			if errors.As(err, &already) {
				return already
			}
			return Fault{Allowance: allowance.Name, Err: err}
		},
	)
}
