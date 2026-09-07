package lifetime

import "context"

// How a caller parks on a queue it cannot serve immediately.
//
// Each waiter owns a channel with room for exactly the one message it will ever
// receive, so a handoff never blocks the goroutine performing it and can be
// done while the queue is locked. Closing that channel instead of sending means
// the queue shut down.
//
// A cancelled waiter must remove itself, and may lose that race against a
// handoff that has already happened. Losing it is not an error: the value was
// delivered, and reporting a cancellation would discard it.

// waitingTaker is a taker parked on an empty queue. Its channel has room for
// exactly the one value it will ever receive, so a handoff never blocks the
// offerer, and closing it instead means the queue shut down.
type waitingTaker[A any] struct {
	ready chan A
}

// waitingOfferer is an offerer parked on a full queue. Closing its channel
// instead of sending means the queue shut down before it had room.
type waitingOfferer[A any] struct {
	value    A
	admitted chan bool
}

func (queue *Queue[A]) awaitRoom(ctx context.Context, waiter *waitingOfferer[A]) (bool, bool) {
	select {
	case admitted := <-waiter.admitted:
		return admitted, false
	case <-ctx.Done():
		if queue.abandonOfferer(waiter) {
			return false, true
		}
		// Room appeared before the cancellation was observed, so the value is
		// already in the queue and reporting otherwise would lose it.
		return <-waiter.admitted, false
	}
}

func (queue *Queue[A]) awaitValue(ctx context.Context, waiter *waitingTaker[A]) (A, bool, bool) {
	select {
	case value, open := <-waiter.ready:
		return value, open, false
	case <-ctx.Done():
		if queue.abandonTaker(waiter) {
			var abandoned A
			return abandoned, false, true
		}
		// A value was handed over before the cancellation was observed.
		value, open := <-waiter.ready
		return value, open, false
	}
}

func (queue *Queue[A]) abandonTaker(waiter *waitingTaker[A]) bool {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	for index, parked := range queue.takers {
		if parked == waiter {
			queue.takers = append(queue.takers[:index], queue.takers[index+1:]...)
			return true
		}
	}
	return false
}

func (queue *Queue[A]) abandonOfferer(waiter *waitingOfferer[A]) bool {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	for index, parked := range queue.offerers {
		if parked == waiter {
			queue.offerers = append(queue.offerers[:index], queue.offerers[index+1:]...)
			return true
		}
	}
	return false
}
