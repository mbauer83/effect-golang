package lifetime

import (
	"context"
	"sync"
)

// Queue is a work queue with the semantics a native Go channel cannot provide:
// shutdown that is safe from any side, a choice of what to do when it is full,
// and batched taking.
//
// Everything else about it is deliberately channel-shaped. Items come out in
// the order they went in, and a shut-down queue drains before it reports that
// it is finished, exactly as a closed channel does.
//
// A value is handed to the longest-waiting taker rather than published to all
// of them, so a queue with many consumers wakes one goroutine per item instead
// of all of them per item.
type Queue[A any] struct {
	mutex    sync.Mutex
	items    []A
	capacity int
	policy   FullQueuePolicy[A]
	takers   []*takeWaiter[A]
	offerers []*offerWaiter[A]
	closed   bool
}

// NewQueue creates a bounded queue whose behaviour when full is policy's.
func NewQueue[A any](capacity int, policy FullQueuePolicy[A]) *Queue[A] {
	if capacity < 1 {
		capacity = 1
	}
	return &Queue[A]{capacity: capacity, policy: policy}
}

// NewUnboundedQueue creates a queue that never refuses a value. Its memory is
// bounded only by its producers, which is why it is a deliberate choice rather
// than the default.
func NewUnboundedQueue[A any]() *Queue[A] {
	return &Queue[A]{capacity: 0, policy: nil}
}

// Size reports how many values are waiting to be taken.
func (queue *Queue[A]) Size() int {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	return len(queue.items)
}

// Offer adds a value. The first result reports whether the queue accepted it,
// which a dropping queue may decline; the second reports that the caller was
// interrupted while waiting for room.
func (queue *Queue[A]) Offer(ctx context.Context, value A) (accepted bool, interrupted bool) {
	queue.mutex.Lock()
	if queue.closed {
		queue.mutex.Unlock()
		return false, false
	}
	if queue.handOff(value) || queue.enqueue(value) {
		queue.mutex.Unlock()
		return true, false
	}

	accepted, waiter := queue.policy.admit(queue, value)
	queue.mutex.Unlock()
	if waiter == nil {
		return accepted, false
	}
	return queue.awaitRoom(ctx, waiter)
}

// handOff gives the value straight to the longest-waiting taker. It runs under
// the lock, which is safe because a taker's channel has room for exactly this
// one value.
func (queue *Queue[A]) handOff(value A) bool {
	if len(queue.takers) == 0 {
		return false
	}
	taker := queue.takers[0]
	queue.takers = queue.takers[1:]
	taker.ready <- value
	return true
}

func (queue *Queue[A]) enqueue(value A) bool {
	if queue.capacity != 0 && len(queue.items) >= queue.capacity {
		return false
	}
	queue.items = append(queue.items, value)
	return true
}

// Take removes the next value. The bool reports whether one was available; it
// is false only once the queue has been shut down and drained, which is the
// same signal a closed channel gives.
func (queue *Queue[A]) Take(ctx context.Context) (A, bool, bool) {
	queue.mutex.Lock()
	if value, ok := queue.dequeue(); ok {
		queue.mutex.Unlock()
		return value, true, false
	}
	if queue.closed {
		queue.mutex.Unlock()
		var zero A
		return zero, false, false
	}

	waiter := &takeWaiter[A]{ready: make(chan A, 1)}
	queue.takers = append(queue.takers, waiter)
	queue.mutex.Unlock()
	return queue.awaitValue(ctx, waiter)
}

// dequeue removes the oldest value and lets one parked offerer into the slot it
// freed. It runs under the lock.
func (queue *Queue[A]) dequeue() (A, bool) {
	if len(queue.items) == 0 {
		var empty A
		return empty, false
	}
	value := queue.items[0]
	queue.items = queue.items[1:]
	queue.admitOneOfferer()
	return value, true
}

func (queue *Queue[A]) admitOneOfferer() {
	if len(queue.offerers) == 0 {
		return
	}
	offerer := queue.offerers[0]
	queue.offerers = queue.offerers[1:]
	queue.items = append(queue.items, offerer.value)
	offerer.admission <- true
}

// TakeAvailable removes up to limit values that are already waiting, without
// blocking. An empty result means the queue was empty at that instant, which is
// a different statement from being finished, so it is the primitive for
// draining a backlog rather than for consuming a stream.
func (queue *Queue[A]) TakeAvailable(limit int) []A {
	if limit < 1 {
		return nil
	}
	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	return queue.drainUpTo(limit, nil)
}

// TakeUpTo removes up to limit values, waiting for at least one. An empty
// result therefore means the queue has been shut down and drained, which is the
// signal a consumer needs and TakeAvailable deliberately cannot give.
func (queue *Queue[A]) TakeUpTo(ctx context.Context, limit int) ([]A, bool) {
	if limit < 1 {
		return nil, false
	}
	first, ok, interrupted := queue.Take(ctx)
	if interrupted {
		return nil, true
	}
	if !ok {
		return nil, false
	}

	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	return queue.drainUpTo(limit, append(make([]A, 0, limit), first)), false
}

// drainUpTo moves waiting values into batch until it holds limit of them or the
// queue is empty. It runs under the lock.
func (queue *Queue[A]) drainUpTo(limit int, batch []A) []A {
	for len(batch) < limit {
		value, available := queue.dequeue()
		if !available {
			break
		}
		batch = append(batch, value)
	}
	return batch
}

// Shutdown stops the queue accepting values and releases everyone waiting on
// it. It is safe from any side and idempotent, which is the difference that
// justifies this type: closing a channel is a single-producer protocol and is
// unsafe by construction once several producers exist.
//
// Values already queued are kept, so a consumer finishes the backlog before it
// sees that the queue is done.
func (queue *Queue[A]) Shutdown() {
	queue.mutex.Lock()
	if queue.closed {
		queue.mutex.Unlock()
		return
	}
	queue.closed = true
	takers, offerers := queue.takers, queue.offerers
	queue.takers, queue.offerers = nil, nil
	queue.mutex.Unlock()

	for _, taker := range takers {
		close(taker.ready)
	}
	for _, offerer := range offerers {
		close(offerer.admission)
	}
}

// IsShutdown reports whether the queue has been shut down.
func (queue *Queue[A]) IsShutdown() bool {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	return queue.closed
}
