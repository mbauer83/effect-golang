package effecttest

import (
	"context"
	"sync"

	"github.com/mbauer83/effect-golang/effect"
)

// Barrier releases each participant only once all of them have arrived.
//
// It is what makes a concurrency assertion honest: a sequential implementation
// deadlocks on a barrier instead of quietly passing a test that only counted
// results.
type Barrier struct {
	participants sync.WaitGroup
}

func NewBarrier(participants int) *Barrier {
	meeting := &Barrier{}
	meeting.participants.Add(participants)
	return meeting
}

// Arrive blocks until every participant has arrived.
func (meeting *Barrier) Arrive() {
	meeting.participants.Done()
	meeting.participants.Wait()
}

// Blocker is cooperative work that reports when it has really begun.
//
// A test that cancels work must observe it running first. Cancelling before it
// starts is also correct runtime behaviour -- the work simply never runs -- but
// it does not exercise interruption.
type Blocker struct {
	started  chan struct{}
	release  chan struct{}
	tracker  *Tracker
	starting sync.Once
}

func NewBlocker(tracker *Tracker) *Blocker {
	return &Blocker{
		started: make(chan struct{}),
		release: make(chan struct{}),
		tracker: tracker,
	}
}

// AwaitStart blocks until the work has begun.
func (work *Blocker) AwaitStart() {
	<-work.started
}

// Release lets the work complete successfully.
func (work *Blocker) Release() {
	close(work.release)
}

// Blocking is an effect that waits until the Blocker is released or its context
// is canceled, recording which happened. It records "completed" on release and
// "interrupted" on cancellation.
func Blocking[R, E, A any](work *Blocker, completed A) effect.Effect[R, E, A] {
	return effect.From(func(ctx context.Context, _ R) effect.Exit[E, A] {
		work.starting.Do(func() { close(work.started) })
		select {
		case <-work.release:
			work.tracker.Record("completed")
			return effect.ExitSuccess[E](completed)
		case <-ctx.Done():
			work.tracker.Record("interrupted")
			return effect.ExitCause[E, A](effect.InterruptCause[E](context.Cause(ctx)))
		}
	})
}

// TrackedResource acquires a named resource in scope and records both its
// acquisition and its release, so a test can assert ordering and exactly-once
// release.
func TrackedResource[R, E any](scope effect.Scope, tracker *Tracker, name string) effect.Effect[R, E, string] {
	return scope.AcquireRelease(
		effect.From(func(context.Context, R) effect.Exit[E, string] {
			tracker.Record("acquire " + name)
			return effect.ExitSuccess[E](name)
		}),
		func(resource string) effect.Effect[R, effect.Never, effect.Unit] {
			return TrackedRelease[R](tracker, "release "+resource)
		},
	)
}

// TrackedRelease is an infallible release workflow that records one event.
func TrackedRelease[R any](tracker *Tracker, event string) effect.Effect[R, effect.Never, effect.Unit] {
	return effect.Release[R](func(context.Context) error {
		tracker.Record(event)
		return nil
	})
}

// Panicking is an effect whose evaluation panics, which the runtime records as
// a defect rather than letting it escape as a typed failure.
func Panicking[R, E, A any](message string) effect.Effect[R, E, A] {
	return effect.From(func(context.Context, R) effect.Exit[E, A] {
		panic(message)
	})
}

// SelfInterrupting is an effect that reports its own interruption, for covering
// the interrupted branch of an outcome table without arranging a cancellation.
func SelfInterrupting[R, E, A any]() effect.Effect[R, E, A] {
	return effect.From(func(context.Context, R) effect.Exit[E, A] {
		return effect.ExitCause[E, A](effect.InterruptCause[E](context.Canceled))
	})
}
