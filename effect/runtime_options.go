package effect

import (
	"errors"

	"github.com/mbauer83/effect-golang/effect/capability"
	"github.com/mbauer83/effect-golang/effect/internal/lifetime"
)

// Clock is the runtime timekeeping port.
type Clock = capability.Clock

// FileSystem is the runtime filesystem port.
type FileSystem = capability.FileSystem

// Logger is the runtime structured-logging port.
type Logger = capability.Logger

// LogRecord is delivered to the configured Logger.
type LogRecord = capability.LogRecord

// Diagnostics is the runtime port for instrumentation faults.
type Diagnostics = capability.Diagnostics

// RuntimeFault describes one instrumentation failure.
type RuntimeFault = capability.RuntimeFault

// FaultComponent names the component whose instrumentation failed.
type FaultComponent = capability.FaultComponent

// Flusher is implemented by a buffering capability that must drain on Close.
type Flusher = capability.Flusher

// LiveWork counts the fibers and scoped resources a Runtime still owns.
type LiveWork = lifetime.LiveWork

type EventKind = capability.EventKind
type EventStatus = capability.EventStatus
type RuntimeEvent = capability.RuntimeEvent
type Observer = capability.Observer

const (
	FaultLogger   = capability.FaultLogger
	FaultObserver = capability.FaultObserver
	FaultRuntime  = capability.FaultRuntime

	EventFiberStarted     = capability.EventFiberStarted
	EventFiberCompleted   = capability.EventFiberCompleted
	EventScopeOpened      = capability.EventScopeOpened
	EventScopeClosing     = capability.EventScopeClosing
	EventScopeClosed      = capability.EventScopeClosed
	EventResourceAcquired = capability.EventResourceAcquired
	EventResourceReleased = capability.EventResourceReleased
	EventRuntimeClosing   = capability.EventRuntimeClosing
	EventRuntimeClosed    = capability.EventRuntimeClosed
	EventSpanStarted      = capability.EventSpanStarted
	EventSpanEnded        = capability.EventSpanEnded
	EventRetryScheduled   = capability.EventRetryScheduled
	EventRetryExhausted   = capability.EventRetryExhausted
	EventRetrySucceeded   = capability.EventRetrySucceeded
	EventRepeatScheduled  = capability.EventRepeatScheduled
	EventRepeatCompleted  = capability.EventRepeatCompleted
	EventLogEmitted       = capability.EventLogEmitted

	EventStatusNone        = capability.EventStatusNone
	EventStatusSuccess     = capability.EventStatusSuccess
	EventStatusFailure     = capability.EventStatusFailure
	EventStatusDefect      = capability.EventStatusDefect
	EventStatusInterrupted = capability.EventStatusInterrupted
)

// runtimeConfig is what a Runtime is built from. Debug tracking has no boolean
// setting: the presence of a ledger is the setting, so a runtime without
// tracking has nothing to count with.
type runtimeConfig struct {
	capabilities capability.Set
	ledger       *lifetime.Ledger
}

// RuntimeOption applies one validated runtime override.
type RuntimeOption interface {
	apply(*runtimeConfig) error
}

type clockOption struct {
	clock Clock
}

// WithClock replaces the Clock for one Runtime.
func WithClock(clock Clock) RuntimeOption {
	return clockOption{clock: clock}
}

func (option clockOption) apply(config *runtimeConfig) error {
	if option.clock == nil {
		return errors.New("effect: Clock capability must not be nil")
	}
	config.capabilities.Clock = option.clock
	return nil
}

type fileSystemOption struct {
	fileSystem FileSystem
}

// WithFileSystem replaces the FileSystem for one Runtime.
func WithFileSystem(fileSystem FileSystem) RuntimeOption {
	return fileSystemOption{fileSystem: fileSystem}
}

func (option fileSystemOption) apply(config *runtimeConfig) error {
	if option.fileSystem == nil {
		return errors.New("effect: FileSystem capability must not be nil")
	}
	config.capabilities.FileSystem = option.fileSystem
	return nil
}

type loggerOption struct {
	logger Logger
}

// WithLogger replaces the Logger for one Runtime.
func WithLogger(logger Logger) RuntimeOption {
	return loggerOption{logger: logger}
}

func (option loggerOption) apply(config *runtimeConfig) error {
	if option.logger == nil {
		return errors.New("effect: Logger capability must not be nil")
	}
	config.capabilities.Logger = option.logger
	return nil
}

// debugTracking is the only option that is not a capability replacement: it
// asks the runtime to count the work it owns.
type debugTracking struct{}

// WithDebugTracking makes the Runtime count the fibers and scoped resources it
// owns, so LiveWork can be asserted on and Close can report work a program
// left running. It costs a pair of atomic counters per fiber and per resource,
// which is why it is not the default.
func WithDebugTracking() RuntimeOption {
	return debugTracking{}
}

func (debugTracking) apply(config *runtimeConfig) error {
	config.ledger = &lifetime.Ledger{}
	return nil
}

type diagnosticsOption struct {
	diagnostics Diagnostics
}

// WithDiagnostics replaces the instrumentation-fault sink for one Runtime.
func WithDiagnostics(diagnostics Diagnostics) RuntimeOption {
	return diagnosticsOption{diagnostics: diagnostics}
}

func (option diagnosticsOption) apply(config *runtimeConfig) error {
	if option.diagnostics == nil {
		return errors.New("effect: Diagnostics capability must not be nil")
	}
	config.capabilities.Diagnostics = option.diagnostics
	return nil
}

type observerOption struct {
	observer Observer
}

// WithObserver installs a runtime-local lifecycle observer.
func WithObserver(observer Observer) RuntimeOption {
	return observerOption{observer: observer}
}

func (option observerOption) apply(config *runtimeConfig) error {
	if option.observer == nil {
		return errors.New("effect: Observer must not be nil")
	}
	config.capabilities.Observer = option.observer
	return nil
}
