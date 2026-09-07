// Package capability defines the runtime ports used by base-package effects.
// Implementations belong to platform or test adapter packages.
package capability

import (
	"context"
	"io/fs"
	"log/slog"
	"time"
)

// Clock supplies current time and interruptible waiting.
type Clock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}

// FileSystem supplies the bounded set of filesystem operations exposed by the
// base effect package.
type FileSystem interface {
	ReadFile(context.Context, string) ([]byte, error)
	WriteFile(context.Context, string, []byte, fs.FileMode) error
	Stat(context.Context, string) (fs.FileInfo, error)
	ReadDir(context.Context, string) ([]fs.DirEntry, error)
	MkdirAll(context.Context, string, fs.FileMode) error
	Remove(context.Context, string) error
	Rename(context.Context, string, string) error
}

// LogRecord is the structured record delivered to a Logger. Field order is
// preserved so renderers and golden tests stay deterministic.
type LogRecord struct {
	Timestamp time.Time
	Level     slog.Level
	Message   string
	Fields    []slog.Attr
	Operation string
	FiberID   uint64
	SpanID    uint64
	ParentID  uint64
}

// Logger receives structured records. Implementations return infrastructure
// failures rather than manufacturing application-domain errors.
type Logger interface {
	Log(context.Context, LogRecord) error
}

// EventKind identifies a meaningful runtime lifecycle boundary.
type EventKind string

const (
	EventFiberStarted     EventKind = "fiber_started"
	EventFiberCompleted   EventKind = "fiber_completed"
	EventScopeOpened      EventKind = "scope_opened"
	EventScopeClosing     EventKind = "scope_closing"
	EventScopeClosed      EventKind = "scope_closed"
	EventResourceAcquired EventKind = "resource_acquired"
	EventResourceReleased EventKind = "resource_released"
	EventRuntimeClosing   EventKind = "runtime_closing"
	EventRuntimeClosed    EventKind = "runtime_closed"
	EventSpanStarted      EventKind = "span_started"
	EventSpanEnded        EventKind = "span_ended"
	EventRetryScheduled   EventKind = "retry_scheduled"
	EventRetryExhausted   EventKind = "retry_exhausted"
	EventRetrySucceeded   EventKind = "retry_succeeded"
	EventRepeatScheduled  EventKind = "repeat_scheduled"
	EventRepeatCompleted  EventKind = "repeat_completed"
	EventLogEmitted       EventKind = "log_emitted"
)

// EventStatus is a bounded terminal classification suitable for metrics.
type EventStatus string

const (
	EventStatusNone        EventStatus = ""
	EventStatusSuccess     EventStatus = "success"
	EventStatusFailure     EventStatus = "typed_failure"
	EventStatusDefect      EventStatus = "defect"
	EventStatusInterrupted EventStatus = "interrupted"
)

// RuntimeEvent contains bounded metadata, never environments or effect values.
type RuntimeEvent struct {
	Kind        EventKind
	Timestamp   time.Time
	Duration    time.Duration
	Operation   string
	Source      string
	FiberID     uint64
	ParentFiber uint64
	SpanID      uint64
	ParentID    uint64
	Attempt     uint64
	Delay       time.Duration
	Status      EventStatus
	Attributes  []slog.Attr
}

// Observer receives runtime events. Implementations must return promptly;
// exporting and buffering policies belong to owned adapters.
type Observer interface {
	Observe(context.Context, RuntimeEvent)
}

// FaultComponent names the runtime component whose instrumentation failed.
type FaultComponent string

const (
	FaultLogger   FaultComponent = "logger"
	FaultObserver FaultComponent = "observer"
	FaultRuntime  FaultComponent = "runtime"
)

// RuntimeFault describes a failure of the runtime's own instrumentation.
type RuntimeFault struct {
	Component FaultComponent
	Operation string
	Err       error
}

// Diagnostics receives instrumentation faults: a Logger that could not deliver
// a record, or an Observer that panicked.
//
// These are library or adapter faults rather than application outcomes, so they
// must never widen an effect's E channel or become one of its defects. A
// Diagnostics implementation is the last resort and must not itself fail.
type Diagnostics interface {
	Report(context.Context, RuntimeFault)
}

// Flusher is implemented by a capability that buffers work and must drain it
// before the owning Runtime exits. Buffering adapters own their queue and
// worker lifetime; the core runtime never spawns one for them.
type Flusher interface {
	Flush(context.Context) error
}

// Set is the immutable capability set observed by one runtime interpretation.
type Set struct {
	Clock       Clock
	FileSystem  FileSystem
	Logger      Logger
	Observer    Observer
	Diagnostics Diagnostics
}
