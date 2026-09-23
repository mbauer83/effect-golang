package effect

import "time"

// Bodies run on parked workers when one is idle.
//
// Most of what a body costs to start is a goroutine growing its stack to what
// an interpretation needs, and a worker has already done that. The hand-off is
// an unbuffered send that succeeds only when a worker is waiting for it, so a
// body is never queued behind another: without an idle worker, a new one
// starts.
//
// A worker whose body failed ended with runtime.Goexit and does not come back.
// One that has waited bodyIdleFor without work exits, so a program that stopped
// running bodies stops holding goroutines for them.
const bodyIdleFor = time.Second

var bodyJobs = make(chan func())

func dispatchBody(job func()) {
	select {
	case bodyJobs <- job:
	default:
		// Handed over rather than passed as an argument: the send parks this
		// goroutine until the worker takes the job, which lets the scheduler
		// run the worker here instead of waking another thread for it.
		first := make(chan func())
		go workBodies(first)
		first <- job
	}
}

func workBodies(first chan func()) {
	job := <-first
	// The timer is made after the first body returns rather than before it,
	// because a body that fails ends this goroutine, and a failed run should not
	// leave a pending timer behind.
	job()
	timer := time.NewTimer(bodyIdleFor)
	for {
		select {
		case job = <-bodyJobs:
		case <-timer.C:
			return
		}
		job()
		timer.Reset(bodyIdleFor)
	}
}
