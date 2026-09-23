// Package rate is permission to proceed no faster than agreed.
//
// The other half of being a good guest on somebody else's service, and it is
// not about HTTP: a program that sends mail, publishes to a broker or walks a
// filesystem has the same thing to arrange, and arranges it the same way.
//
// A reservation rather than a check, and that distinction is the whole design.
// A check reports whether there is room now, and ten callers asking at once
// are all told yes. A reservation hands each caller a moment that nothing else
// has been given, so a caller that waits for its moment and then proceeds is
// within the rate whatever else is happening beside it.
//
// Not a runtime capability. A program has one clock; it has one allowance per
// service it reads, and those are values it holds.
package rate

import (
	"context"
	"time"
)

// Allowance is what something permits: how many in how long, counted under a
// name.
//
// A value the caller states rather than configuration of a limiter, so two
// services counted by one limiter cannot be confused for one, and a service
// cannot be read without saying the rate it may be read at.
type Allowance struct {
	Name  string
	Most  int
	Every time.Duration
}

// IsStated reports whether this is an allowance at all.
func (allowance Allowance) IsStated() bool {
	return allowance.Name != "" && allowance.Most > 0 && allowance.Every > 0
}

// Spacing is how far apart requests are once the burst is spent.
func (allowance Allowance) Spacing() time.Duration {
	if !allowance.IsStated() {
		return 0
	}
	return allowance.Every / time.Duration(allowance.Most)
}

// Limiter hands out turns.
//
// One method, because there is one question, and it is asked in the plainest
// shape an adapter can implement: context, two durations, an error.
// Everything about waiting for the turn is in Waiting, which is the only
// thing in this package that knows about effects.
type Limiter interface {
	// Turn reserves the next turn under an allowance and says how long until
	// it: nothing when there is room now, and the wait until the reserved
	// moment when there is not.
	//
	// It reserves nothing and answers ErrLimitExceeded when the turn would be later
	// than longest, and that decision is here rather than in the caller
	// because it cannot be made anywhere else: a caller that asked for a turn
	// and then declined to wait for it would have spent an allowance on a
	// request it never made, and would have pushed back every caller behind
	// it. Deciding and reserving have to be one operation, and only the
	// limiter can make them one. A longest of zero or less has no ceiling and
	// always reserves.
	//
	// It returns the wait rather than performing it so that the waiting
	// happens in the interpretation, where the runtime's clock and its
	// cancellation are: a caller abandoned while waiting for its turn is
	// abandoned, and a test can move time rather than spend it.
	//
	// The wait is reported either way, so a caller refused a turn can say how
	// long the queue was rather than only that there was one.
	Turn(
		ctx context.Context,
		allowance Allowance,
		longest time.Duration,
	) (time.Duration, error)
}

// Fault is why a limiter could not hand out a turn.
type Fault struct {
	Allowance string
	Err       error
}

func (fault Fault) Error() string {
	message := "rate: " + fault.Allowance
	if fault.Err != nil {
		message += ": " + fault.Err.Error()
	}
	return message
}

// Unwrap keeps the limiter's own error reachable, so a caller can tell a
// limiter that is unreachable from a queue it will not join.
func (fault Fault) Unwrap() error { return fault.Err }
