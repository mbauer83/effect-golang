package lifetime

// FullQueuePolicy decides what a bounded queue does with a value it has no room
// for.
//
// The three answers behave differently enough that a flag would hide the
// difference, so each is its own type. admit runs while the queue is locked and
// full; returning a waiter means the offer must park until room appears. The
// method stays unexported, so only this package can implement the interface.
type FullQueuePolicy[A any] interface {
	admit(queue *Queue[A], value A) (accepted bool, waiter *offerWaiter[A])
}

// BackPressure makes an offer wait until room appears, which is the only policy
// that applies backpressure to the producer.
func BackPressure[A any]() FullQueuePolicy[A] {
	return backPressure[A]{}
}

// DropNewest refuses the incoming value, keeping the backlog intact.
func DropNewest[A any]() FullQueuePolicy[A] {
	return dropNewest[A]{}
}

// DropOldest discards the oldest queued value to make room, keeping the
// most recent, which is what a queue of current-state updates wants.
func DropOldest[A any]() FullQueuePolicy[A] {
	return dropOldest[A]{}
}

type backPressure[A any] struct{}

func (backPressure[A]) admit(queue *Queue[A], value A) (bool, *offerWaiter[A]) {
	waiter := &offerWaiter[A]{value: value, admission: make(chan bool, 1)}
	queue.offerers = append(queue.offerers, waiter)
	return false, waiter
}

type dropNewest[A any] struct{}

func (dropNewest[A]) admit(*Queue[A], A) (bool, *offerWaiter[A]) {
	return false, nil
}

type dropOldest[A any] struct{}

func (dropOldest[A]) admit(queue *Queue[A], value A) (bool, *offerWaiter[A]) {
	queue.items = append(queue.items[1:], value)
	return true, nil
}
