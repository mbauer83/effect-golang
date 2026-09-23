// Package filecopy contains a complete base-capability example program.
package filecopy

import (
	"log/slog"
	"strings"
	"time"

	"github.com/mbauer83/effect-golang/effect"
)

type sourceSnapshot struct {
	content   []byte
	timestamp time.Time
}

// Program reads a text file, normalizes it to uppercase, records the operation,
// and writes the result. All behavior remains suspended until interpretation.
func Program(inputPath string, outputPath string) effect.Effect[effect.Unit, effect.IOError, effect.Unit] {
	operations := effect.IO()
	return copyProgram(operations, inputPath, outputPath, operations.ReadFile(inputPath))
}

// ProgramWithRetry is Program with an explicit retry policy around source reads.
func ProgramWithRetry[Out any](
	inputPath string,
	outputPath string,
	policy effect.Schedule[effect.IOError, Out],
) effect.Effect[effect.Unit, effect.IOError, effect.Unit] {
	operations := effect.IO()
	read := operations.ReadFile(inputPath).WithName("read-source").Retry(policy)
	return copyProgram(operations, inputPath, outputPath, read)
}

func copyProgram(
	operations effect.IOOperations[effect.Unit],
	inputPath string,
	outputPath string,
	read effect.Effect[effect.Unit, effect.IOError, []byte],
) effect.Effect[effect.Unit, effect.IOError, effect.Unit] {
	snapshot := effect.Zip(
		read,
		operations.Now(),
	).Map(func(values effect.Product[[]byte, time.Time]) sourceSnapshot {
		return sourceSnapshot{content: values.First, timestamp: values.Second}
	})

	return snapshot.
		FlatMap(normalizeAndStore(operations, inputPath, outputPath)).
		WithName("file-copy").
		WithSpan("file-copy", slog.String("input", inputPath))
}

func normalizeAndStore(
	operations effect.IOOperations[effect.Unit],
	inputPath string,
	outputPath string,
) func(sourceSnapshot) effect.Effect[effect.Unit, effect.IOError, effect.Unit] {
	return func(source sourceSnapshot) effect.Effect[effect.Unit, effect.IOError, effect.Unit] {
		// The enclosing span already annotates the input path, and inherited
		// annotations are prepended to a record's own fields, so repeating it
		// here would emit the key twice.
		message := operations.LogInfo(
			"normalizing file",
			slog.Int("bytes", len(source.content)),
			slog.Time("started_at", source.timestamp),
		)
		write := operations.WriteFile(
			outputPath,
			[]byte(strings.ToUpper(string(source.content))),
			0o600,
		)
		return message.AndThen(write)
	}
}
