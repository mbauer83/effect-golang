package effect

import (
	"context"
	"log/slog"
	"slices"

	"github.com/mbauer83/effect-golang/capability"
	runtimecore "github.com/mbauer83/effect-golang/internal/runtime"
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
