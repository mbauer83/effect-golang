package effecttest

import (
	"context"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang/effect"
)

// NewTimedRuntime builds a Runtime whose every wait goes through a manually
// advanced clock, and closes it when the test finishes.
//
// The clock override belongs to this Runtime alone, so tests that use different
// clocks can run in parallel without interfering.
func NewTimedRuntime(t testing.TB, options ...effect.RuntimeOption) (*effect.Runtime, *ManualClock) {
	t.Helper()
	clock := NewManualClock(time.Unix(0, 0))
	runtime, err := effect.NewRuntime(append([]effect.RuntimeOption{effect.WithClock(clock)}, options...)...)
	if err != nil {
		t.Fatalf("effecttest: could not build a timed runtime: %v", err)
	}
	t.Cleanup(func() {
		runtime.Close(context.Background())
	})
	return runtime, clock
}
