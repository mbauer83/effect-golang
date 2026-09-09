package rate

// A limiter in this process.

import (
	"context"
	"sync"
	"time"
)

// Held hands out turns within one process.
//
// Correct for a program that runs as one instance, and honestly wrong for one
// that runs as four: each would keep its own count and the four together would
// ask at four times the rate one of them agreed to. That is what a shared
// limiter is for, and this is what a single instance and every test needs.
type Held struct {
	mutex    sync.Mutex
	now      func() time.Time
	arriving map[string]time.Time
}

// NewHeld is a limiter on this clock.
//
// The clock is a parameter because a rate is a thing a test has to be able to
// move: waiting out a minute's allowance is not a test. Pass time.Now unless
// you are one.
func NewHeld(now func() time.Time) *Held {
	if now == nil {
		now = time.Now
	}
	return &Held{now: now, arriving: map[string]time.Time{}}
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
func (limiter *Held) Turn(_ context.Context, allowance Allowance) (time.Duration, error) {
	if !allowance.IsStated() {
		return 0, Fault{Allowance: allowance.Name, Err: ErrUnstated}
	}
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	spacing := allowance.Spacing()
	now := limiter.now()
	arriving := limiter.arriving[allowance.Name]
	if arriving.Before(now) {
		arriving = now
	}
	tolerance := time.Duration(allowance.Most-1) * spacing
	wait := arriving.Add(-tolerance).Sub(now)
	limiter.arriving[allowance.Name] = arriving.Add(spacing)
	if wait < 0 {
		return 0, nil
	}
	return wait, nil
}
