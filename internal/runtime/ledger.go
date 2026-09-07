package runtime

import "sync/atomic"

// Ledger counts the work a runtime still owns.
//
// It exists for leak assertions in tests and for shutdown diagnostics, so it is
// installed only when a runtime asks for debug tracking. Every method tolerates
// a nil receiver, which is how a runtime without tracking pays nothing beyond a
// nil check.
type Ledger struct {
	fibersStarted   atomic.Int64
	fibersCompleted atomic.Int64
	acquired        atomic.Int64
	released        atomic.Int64
}

// LiveWork is a snapshot of work that has been started or acquired and has not
// yet finished or been released.
type LiveWork struct {
	Fibers    int64
	Resources int64
}

// IsEmpty reports whether the runtime owns no unfinished work.
func (work LiveWork) IsEmpty() bool {
	return work.Fibers == 0 && work.Resources == 0
}

func (ledger *Ledger) FiberStarted() {
	if ledger != nil {
		ledger.fibersStarted.Add(1)
	}
}

func (ledger *Ledger) FiberCompleted() {
	if ledger != nil {
		ledger.fibersCompleted.Add(1)
	}
}

func (ledger *Ledger) ResourceAcquired() {
	if ledger != nil {
		ledger.acquired.Add(1)
	}
}

func (ledger *Ledger) ResourceReleased() {
	if ledger != nil {
		ledger.released.Add(1)
	}
}

// Live returns the current snapshot. A nil ledger reports nothing live, which
// is the honest answer when tracking was never enabled.
func (ledger *Ledger) Live() LiveWork {
	if ledger == nil {
		return LiveWork{}
	}
	return LiveWork{
		Fibers:    ledger.fibersStarted.Load() - ledger.fibersCompleted.Load(),
		Resources: ledger.acquired.Load() - ledger.released.Load(),
	}
}
