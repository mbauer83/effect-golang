package rate

// A limiter in this process.

import (
	"context"
	"sync"
	"time"
)

// MemoryLimiter hands out turns within one process.
//
// Correct for a program that runs as one instance, and honestly wrong for one
// that runs as four: each would keep its own count and the four together would
// ask at four times the rate one of them agreed to. That is what a shared
// limiter is for, and this is what a single instance and every test needs.
type MemoryLimiter struct {
	mutex       sync.Mutex
	now         func() time.Time
	nextArrival map[string]time.Time
}

// NewMemoryLimiter is a limiter on this clock.
//
// The clock is a parameter because a rate is a thing a test has to be able to
// move: waiting out a minute's allowance is not a test. Pass time.Now unless
// you are one.
func NewMemoryLimiter(now func() time.Time) *MemoryLimiter {
	if now == nil {
		now = time.Now
	}
	return &MemoryLimiter{now: now, nextArrival: map[string]time.Time{}}
}

// Turn reserves the next turn under an allowance and says how long until it.
//
// The generic cell rate algorithm, which is what a rate limit that must not be
// exceeded looks like written down. One moment is kept per allowance: when the
// next request would arrive if requests were spaced evenly. A caller moves
// that moment along by one spacing and is told to wait until the moment it
// just claimed, less the burst the allowance tolerates.
//
// So the first Most requests in a period go at once and everything after them
// is spaced -- which is what "forty a minute" means to whoever is being asked
// forty times -- and a spent allowance comes back one turn at a time rather
// than all at once on a window boundary.
//
// The ceiling is checked before the moment is moved along, under the same
// lock, so a caller that will not wait leaves the allowance exactly as it
// found it.
func (limiter *MemoryLimiter) Turn(
	_ context.Context,
	allowance Allowance,
	longest time.Duration,
) (time.Duration, error) {
	if !allowance.IsStated() {
		return 0, Fault{Allowance: allowance.Name, Err: ErrUnstated}
	}
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	spacing := allowance.Spacing()
	now := limiter.now()
	nextArrival := limiter.nextArrival[allowance.Name]
	if nextArrival.Before(now) {
		nextArrival = now
	}
	tolerance := time.Duration(allowance.Most-1) * spacing
	wait := nextArrival.Add(-tolerance).Sub(now)
	if wait < 0 {
		wait = 0
	}
	if longest > 0 && wait > longest {
		return wait, Fault{Allowance: allowance.Name, Err: ErrLimitExceeded}
	}
	limiter.nextArrival[allowance.Name] = nextArrival.Add(spacing)
	return wait, nil
}
