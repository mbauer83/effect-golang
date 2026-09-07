package platform

import (
	"context"
	"time"
)

// LiveClock delegates timekeeping and waiting to Go's standard library.
type LiveClock struct{}

func (LiveClock) Now() time.Time {
	return time.Now()
}

func (LiveClock) Sleep(ctx context.Context, duration time.Duration) error {
	if duration < 0 {
		duration = 0
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}
