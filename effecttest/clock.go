// Package effecttest provides deterministic runtime capability adapters.
package effecttest

import (
	"context"
	"sync"
	"testing"
	"time"
)

// ManualClock advances only when the test asks it to.
type ManualClock struct {
	mu       sync.Mutex
	now      time.Time
	sleepers []*clockSleeper
	changed  chan struct{}
}

type clockSleeper struct {
	deadline time.Time
	ready    chan struct{}
}

func NewManualClock(start time.Time) *ManualClock {
	return &ManualClock{now: start, changed: make(chan struct{})}
}

func (clock *ManualClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *ManualClock) Sleep(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}

	sleeper := &clockSleeper{ready: make(chan struct{})}
	clock.mu.Lock()
	sleeper.deadline = clock.now.Add(duration)
	clock.sleepers = append(clock.sleepers, sleeper)
	clock.signalChange()
	clock.mu.Unlock()

	select {
	case <-sleeper.ready:
		return nil
	case <-ctx.Done():
		if clock.remove(sleeper) {
			return context.Cause(ctx)
		}
		<-sleeper.ready
		return nil
	}
}

func (clock *ManualClock) remove(target *clockSleeper) bool {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	for index, sleeper := range clock.sleepers {
		if sleeper == target {
			clock.sleepers = append(clock.sleepers[:index], clock.sleepers[index+1:]...)
			clock.signalChange()
			return true
		}
	}
	return false
}

// Advance moves time forward and wakes every due sleeper.
func (clock *ManualClock) Advance(duration time.Duration) {
	if duration < 0 {
		panic("effecttest: ManualClock cannot move backwards")
	}

	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(duration)
	pending := clock.sleepers[:0]
	for _, sleeper := range clock.sleepers {
		if sleeper.deadline.After(clock.now) {
			pending = append(pending, sleeper)
		} else {
			close(sleeper.ready)
		}
	}
	clock.sleepers = pending
	clock.signalChange()
}

// PendingSleeps reports the number of blocked sleeps.
func (clock *ManualClock) PendingSleeps() int {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return len(clock.sleepers)
}

// WaitForPending blocks until at least count sleeps have registered or ctx is
// canceled. It lets retry and scheduling tests coordinate without real time.
func (clock *ManualClock) WaitForPending(ctx context.Context, count int) error {
	if count < 0 {
		panic("effecttest: pending sleep count cannot be negative")
	}

	for {
		clock.mu.Lock()
		if len(clock.sleepers) >= count {
			clock.mu.Unlock()
			return nil
		}
		changed := clock.changed
		clock.mu.Unlock()

		select {
		case <-changed:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
}

func (clock *ManualClock) signalChange() {
	close(clock.changed)
	clock.changed = make(chan struct{})
}

// AwaitSleepers blocks until at least count waits have registered, and fails
// the test instead of hanging when they never do.
//
// A scheduling test must not advance the clock before the work it is driving
// has actually parked, or the advance is lost and the test becomes a race.
func (clock *ManualClock) AwaitSleepers(t testing.TB, count int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := clock.WaitForPending(ctx, count); err != nil {
		t.Fatalf("effecttest: expected %d pending sleep(s): %v", count, err)
	}
}
