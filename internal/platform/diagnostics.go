package platform

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/mbauer83/effect-golang/capability"
)

// LiveDiagnostics writes instrumentation faults to Output, or to standard error
// when Output is nil.
//
// It deliberately does not route through the configured Logger, because a
// Logger that cannot deliver a record is one of the faults it must report. The
// message is assembled before a single Write so concurrent reports interleave
// per fault rather than per fragment.
type LiveDiagnostics struct {
	Output io.Writer
}

func (diagnostics LiveDiagnostics) Report(_ context.Context, fault capability.RuntimeFault) {
	output := diagnostics.Output
	if output == nil {
		output = os.Stderr
	}
	_, _ = io.WriteString(output, fmt.Sprintf(
		"effect: %s fault during %q: %v\n",
		fault.Component,
		fault.Operation,
		fault.Err,
	))
}
