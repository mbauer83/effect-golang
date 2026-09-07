package lifetime

// FullQueuePolicy decides what a bounded queue does with a value it has no room
// for.
//
// The three answers behave differently enough that a flag would hide the
// difference, so each is its own type. admit runs while the queue is locked and
// full; returning a waiter means the offer must park until room appears. The
// method stays unexported, so only this package can implement the interface.
type FullQueuePolicy[A any] interface {
	admit(queue *Queue[A], value A) (accepted bool, waiter *waitingOfferer[A])
}

// Suspending makes an offer wait until room appears, which is the only policy
// that applies backpressure to the producer.
func Suspending[A any]() FullQueuePolicy[A] {
	return suspending[A]{}
}

// DroppingNewest refuses the incoming value, keeping the backlog intact.
func DroppingNewest[A any]() FullQueuePolicy[A] {
	return droppingNewest[A]{}
}

// DroppingOldest discards the oldest queued value to make room, keeping the
// most recent, which is what a queue of current-state updates wants.
func DroppingOldest[A any]() FullQueuePolicy[A] {
	return droppingOldest[A]{}
}

type suspending[A any] struct{}

func (suspending[A]) admit(queue *Queue[A], value A) (bool, *waitingOfferer[A]) {
	waiter := &waitingOfferer[A]{value: value, admitted: make(chan bool, 1)}
	queue.offerers = append(queue.offerers, waiter)
	return false, waiter
}

type droppingNewest[A any] struct{}

func (droppingNewest[A]) admit(*Queue[A], A) (bool, *waitingOfferer[A]) {
	return false, nil
}

type droppingOldest[A any] struct{}

func (droppingOldest[A]) admit(queue *Queue[A], value A) (bool, *waitingOfferer[A]) {
	queue.items = append(queue.items[1:], value)
	return true, nil
}
