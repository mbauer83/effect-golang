package lifetime

import (
	"context"
	"sort"
	"sync"
)

// Hub broadcasts each published value to every current subscriber.
//
// This is the one thing a native Go channel cannot do at all: a channel
// delivers each value to exactly one receiver, so broadcasting means fanning
// out by hand and getting per-subscriber backpressure wrong.
//
// Each subscriber owns a queue, so a subscriber that stops taking affects only
// its own inbox under a dropping policy, and applies backpressure to the
// publisher under a suspending one. Subscribers see values published after they
// subscribed; there is no replay, because keeping history would make the hub's
// memory a function of how long the program has run.
type Hub[A any] struct {
	mutex    sync.Mutex
	inboxes  map[uint64]*Queue[A]
	nextID   uint64
	capacity int
	policy   FullQueuePolicy[A]
	closed   bool
}

// NewHub creates a hub whose subscribers each get an inbox of the given
// capacity, behaving as policy says when that inbox is full.
func NewHub[A any](capacity int, policy FullQueuePolicy[A]) *Hub[A] {
	return &Hub[A]{inboxes: map[uint64]*Queue[A]{}, capacity: capacity, policy: policy}
}

// Subscribe adds an inbox and returns it with the token that removes it. The
// bool reports whether the hub accepted the subscription; a shut-down hub does
// not.
func (hub *Hub[A]) Subscribe() (*Queue[A], uint64, bool) {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()
	if hub.closed {
		return nil, 0, false
	}

	hub.nextID++
	token := hub.nextID
	inbox := NewQueue(hub.capacity, hub.policy)
	hub.inboxes[token] = inbox
	return inbox, token, true
}

// Unsubscribe removes an inbox and shuts it down, so anything still taking
// from it learns that it is finished rather than waiting forever.
func (hub *Hub[A]) Unsubscribe(token uint64) {
	hub.mutex.Lock()
	inbox, subscribed := hub.inboxes[token]
	delete(hub.inboxes, token)
	hub.mutex.Unlock()

	if subscribed {
		inbox.Shutdown()
	}
}

// Publish delivers value to every current subscriber. The first result reports
// whether the hub accepted it, which a shut-down hub does not; the second
// reports that the caller was interrupted while a suspending subscriber had no
// room.
//
// Delivery is sequential in subscription order, and a suspending subscriber
// therefore delays the ones after it. That is the price of backpressure: choose
// a dropping policy when one slow subscriber must not hold up the others.
func (hub *Hub[A]) Publish(ctx context.Context, value A) (accepted bool, interrupted bool) {
	inboxes, open := hub.currentInboxes()
	if !open {
		return false, false
	}

	for _, inbox := range inboxes {
		// An inbox unsubscribed since the snapshot has been shut down and
		// declines the value, which is exactly the right outcome.
		if _, wasInterrupted := inbox.Offer(ctx, value); wasInterrupted {
			return false, true
		}
	}
	return true, false
}

// currentInboxes snapshots the subscribers in subscription order, so publishing
// never holds the hub's lock while a suspending inbox has no room.
func (hub *Hub[A]) currentInboxes() ([]*Queue[A], bool) {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()
	if hub.closed {
		return nil, false
	}

	tokens := make([]uint64, 0, len(hub.inboxes))
	for token := range hub.inboxes {
		tokens = append(tokens, token)
	}
	sort.Slice(tokens, func(left, right int) bool { return tokens[left] < tokens[right] })

	inboxes := make([]*Queue[A], 0, len(tokens))
	for _, token := range tokens {
		inboxes = append(inboxes, hub.inboxes[token])
	}
	return inboxes, true
}

// Shutdown stops the hub accepting values and shuts down every inbox, so each
// subscriber drains what it already has and then learns the hub is finished.
// It is safe from any side and idempotent.
func (hub *Hub[A]) Shutdown() {
	hub.mutex.Lock()
	if hub.closed {
		hub.mutex.Unlock()
		return
	}
	hub.closed = true
	inboxes := make([]*Queue[A], 0, len(hub.inboxes))
	for _, inbox := range hub.inboxes {
		inboxes = append(inboxes, inbox)
	}
	hub.inboxes = map[uint64]*Queue[A]{}
	hub.mutex.Unlock()

	for _, inbox := range inboxes {
		inbox.Shutdown()
	}
}

// Subscribers reports how many inboxes the hub is currently delivering to.
func (hub *Hub[A]) Subscribers() int {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()
	return len(hub.inboxes)
}

// IsShutdown reports whether the hub has been shut down.
func (hub *Hub[A]) IsShutdown() bool {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()
	return hub.closed
}
