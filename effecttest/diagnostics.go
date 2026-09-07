package effecttest

import (
	"context"
	"slices"
	"sync"

	"github.com/mbauer83/effect-golang/effect"
)

// RecordingDiagnostics stores instrumentation faults for assertions.
//
// A test that deliberately breaks a logger or an observer should install it, so
// the fault is asserted on rather than printed to standard error by the live
// diagnostics sink.
type RecordingDiagnostics struct {
	mutex  sync.Mutex
	faults []effect.RuntimeFault
}

func (diagnostics *RecordingDiagnostics) Report(_ context.Context, fault effect.RuntimeFault) {
	diagnostics.mutex.Lock()
	defer diagnostics.mutex.Unlock()
	diagnostics.faults = append(diagnostics.faults, fault)
}

// Faults returns a defensive snapshot in reporting order.
func (diagnostics *RecordingDiagnostics) Faults() []effect.RuntimeFault {
	diagnostics.mutex.Lock()
	defer diagnostics.mutex.Unlock()
	return slices.Clone(diagnostics.faults)
}
