package effecttest

import (
	"context"
	"slices"
	"sync"

	effect "github.com/mbauer83/effect-golang"
)

// RecordingObserver stores runtime events in delivery order for assertions.
type RecordingObserver struct {
	mu     sync.Mutex
	events []effect.RuntimeEvent
}

func (observer *RecordingObserver) Observe(_ context.Context, event effect.RuntimeEvent) {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	event.Attributes = slices.Clone(event.Attributes)
	observer.events = append(observer.events, event)
}

// Events returns a defensive event snapshot.
func (observer *RecordingObserver) Events() []effect.RuntimeEvent {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	return slices.Clone(observer.events)
}
