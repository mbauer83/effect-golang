package effecttest

import (
	"context"
	"slices"
	"sync"

	"github.com/mbauer83/effect-golang/effect"
)

// LogRecorder stores structured records for deterministic assertions.
type LogRecorder struct {
	mu      sync.Mutex
	records []effect.LogRecord
}

func (logger *LogRecorder) Log(_ context.Context, record effect.LogRecord) error {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	record.Fields = slices.Clone(record.Fields)
	logger.records = append(logger.records, record)
	return nil
}

// Records returns a snapshot that callers may mutate safely.
func (logger *LogRecorder) Records() []effect.LogRecord {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	records := slices.Clone(logger.records)
	for index := range records {
		records[index].Fields = slices.Clone(records[index].Fields)
	}
	return records
}
