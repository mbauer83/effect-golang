package effecttest

import (
	"slices"
	"sync"
)

// Tracker records ordered lifecycle events from concurrent test programs.
//
// Resource and fiber assertions need to check both the order of operations and
// how many times each one happened, and the work under test may run on several
// goroutines, so recording is synchronized.
type Tracker struct {
	mutex  sync.Mutex
	events []string
}

// Record appends one event.
func (tracker *Tracker) Record(event string) {
	tracker.mutex.Lock()
	defer tracker.mutex.Unlock()
	tracker.events = append(tracker.events, event)
}

// Events returns a snapshot in recording order.
func (tracker *Tracker) Events() []string {
	tracker.mutex.Lock()
	defer tracker.mutex.Unlock()
	return slices.Clone(tracker.events)
}

// Count reports how many times event was recorded.
func (tracker *Tracker) Count(event string) int {
	tracker.mutex.Lock()
	defer tracker.mutex.Unlock()
	total := 0
	for _, entry := range tracker.events {
		if entry == event {
			total++
		}
	}
	return total
}
