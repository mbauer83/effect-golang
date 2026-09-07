package platform

import (
	"context"
	"log/slog"

	"github.com/mbauer83/effect-golang/capability"
)

// LiveLogger delivers records to a slog Handler.
type LiveLogger struct {
	Handler slog.Handler
}

func (logger LiveLogger) Log(ctx context.Context, record capability.LogRecord) error {
	if !logger.Handler.Enabled(ctx, record.Level) {
		return nil
	}

	slogRecord := slog.NewRecord(record.Timestamp, record.Level, record.Message, 0)
	slogRecord.AddAttrs(record.Fields...)
	return logger.Handler.Handle(ctx, slogRecord)
}
