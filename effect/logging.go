package effect

import (
	"context"
	"log/slog"
	"slices"

	"github.com/mbauer83/effect-golang/effect/capability"
	runtimecore "github.com/mbauer83/effect-golang/effect/internal/runtime"
)

// Log emits one structured record through the runtime Logger. R and E are
// phantom channels that allow the infallible operation to compose directly.
func Log[R, E any](level slog.Level, message string, fields ...slog.Attr) Effect[R, E, Unit] {
	ownedFields := slices.Clone(fields)
	return fromRuntime(func(ctx context.Context, state *runtimecore.State, _ R) Exit[E, Unit] {
		capabilities := state.Capabilities()
		metadata := state.Metadata()
		record := capability.LogRecord{
			Timestamp: capabilities.Clock.Now(),
			Level:     level,
			Message:   message,
			Fields:    append(metadata.Attributes, ownedFields...),
			Operation: metadata.Operation,
			FiberID:   metadata.FiberID,
			SpanID:    metadata.SpanID,
			ParentID:  metadata.ParentID,
		}

		// Logging is infallible in the E channel. A sink that cannot deliver is
		// an infrastructure fault, so it is reported to diagnostics instead of
		// being turned into an application failure or a defect.
		if err := capabilities.Logger.Log(ctx, record); err != nil {
			state.Report(ctx, capability.RuntimeFault{
				Component: capability.FaultLogger,
				Operation: "log",
				Err:       err,
			})
			return ExitSuccess[E](Unit{})
		}
		if state.Observing() {
			event := state.Event(capability.EventLogEmitted)
			event.Timestamp = record.Timestamp
			event.Status = capability.EventStatusSuccess
			state.Emit(ctx, event)
		}
		return ExitSuccess[E](Unit{})
	})
}

func LogDebug[R, E any](message string, fields ...slog.Attr) Effect[R, E, Unit] {
	return Log[R, E](slog.LevelDebug, message, fields...)
}

func LogInfo[R, E any](message string, fields ...slog.Attr) Effect[R, E, Unit] {
	return Log[R, E](slog.LevelInfo, message, fields...)
}

func LogWarn[R, E any](message string, fields ...slog.Attr) Effect[R, E, Unit] {
	return Log[R, E](slog.LevelWarn, message, fields...)
}

func LogError[R, E any](message string, fields ...slog.Attr) Effect[R, E, Unit] {
	return Log[R, E](slog.LevelError, message, fields...)
}

// Operations carries these channels into the operations below, whose own
// requirement channel is unused and whose failure channel is Never. Selecting
// the channels once keeps a program composable with FlatMap instead of forcing
// a widening at every step. The precise, narrower forms remain available.

// Log emits one structured record using these channels.
func (Operations[R, E]) Log(level slog.Level, message string, fields ...slog.Attr) Effect[R, E, Unit] {
	return Log[R, E](level, message, fields...)
}

// LogDebug emits a debug record using these channels.
func (operations Operations[R, E]) LogDebug(message string, fields ...slog.Attr) Effect[R, E, Unit] {
	return operations.Log(slog.LevelDebug, message, fields...)
}

// LogInfo emits an informational record using these channels.
func (operations Operations[R, E]) LogInfo(message string, fields ...slog.Attr) Effect[R, E, Unit] {
	return operations.Log(slog.LevelInfo, message, fields...)
}

// LogWarn emits a warning record using these channels.
func (operations Operations[R, E]) LogWarn(message string, fields ...slog.Attr) Effect[R, E, Unit] {
	return operations.Log(slog.LevelWarn, message, fields...)
}

// LogError emits an error record using these channels.
func (operations Operations[R, E]) LogError(message string, fields ...slog.Attr) Effect[R, E, Unit] {
	return operations.Log(slog.LevelError, message, fields...)
}
