// Package pipeline is a complete program showing that native Go channels
// remain native.
//
// A producer fiber reads a file, splits it into records and sends them on an
// ordinary buffered channel, closing it when the source is exhausted. The
// consumer folds the records. Channel closure is not a failure here, because it
// is not a failure in Go: it is the producer's way of saying it is finished.
// The caller waits with a plain select over the producer's completion signal
// and its own channel, so the producer's failure cannot be mistaken for an
// empty stream.
package pipeline

import (
	"context"
	"strings"

	"github.com/mbauer83/effect-golang/effect"
)

// Summary is the folded outcome of one pipeline run.
type Summary struct {
	Records int
	Words   int
}

type pipelineEffect[A any] = effect.Effect[effect.Unit, effect.IOError, A]

// Program streams one file's records through a channel and folds them.
//
// The producer owns the channel and is the only side that closes it, which is
// the conventional Go rule the runtime deliberately does not try to replace.
func Program(inputPath string, buffer int) pipelineEffect[Summary] {
	io := effect.IO()
	return effect.Scoped(func(effect.Scope) pipelineEffect[Summary] {
		records := make(chan string, buffer)
		return io.Fork(produce(io, inputPath, records)).
			FlatMap(func(producer effect.Fiber[effect.IOError, effect.Unit]) pipelineEffect[Summary] {
				return consume(io, records, producer)
			}).
			WithName("pipeline")
	})
}

// produce sends every record and then closes the channel exactly once. It is
// forked into the enclosing scope, so it cannot outlive the pipeline even if
// the consumer stops early.
func produce(io effect.IOOperations[effect.Unit], inputPath string, records chan string) pipelineEffect[effect.Unit] {
	return io.ReadFile(inputPath).
		FlatMap(func(content []byte) pipelineEffect[effect.Unit] {
			return effect.ForEach(splitRecords(content), func(record string) pipelineEffect[effect.Unit] {
				return io.Send(records, record)
			}).As(effect.Unit{})
		}).
		Ensuring(closeRecords(records)).
		WithName("produce")
}

// closeRecords runs in every outcome, so a failing producer still releases a
// consumer that is blocked on a receive.
func closeRecords(records chan string) effect.Effect[effect.Unit, effect.Never, effect.Unit] {
	return effect.AddFinalizer[effect.Unit](func(context.Context) error {
		close(records)
		return nil
	})
}

// consume folds records until the channel is drained, then adopts whatever the
// producer reported. Selecting on the fiber's completion signal alongside the
// channel is why a producer failure surfaces instead of looking like the end of
// a short stream.
func consume(
	io effect.IOOperations[effect.Unit],
	records <-chan string,
	producer effect.Fiber[effect.IOError, effect.Unit],
) pipelineEffect[Summary] {
	return foldRecords(io, records, Summary{}).
		FlatMap(func(summary Summary) pipelineEffect[Summary] {
			return io.Join(producer).As(summary)
		}).
		WithName("consume")
}

func foldRecords(
	io effect.IOOperations[effect.Unit],
	records <-chan string,
	summary Summary,
) pipelineEffect[Summary] {
	return io.Recv(records).FlatMap(func(receive effect.Receive[string]) pipelineEffect[Summary] {
		if !receive.OK {
			return io.Succeed(summary)
		}
		return foldRecords(io, records, count(summary, receive.Value))
	})
}

func count(summary Summary, record string) Summary {
	return Summary{
		Records: summary.Records + 1,
		Words:   summary.Words + len(strings.Fields(record)),
	}
}

func splitRecords(content []byte) []string {
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	records := make([]string, 0, len(lines))
	for _, line := range lines {
		if record := strings.TrimSpace(line); record != "" {
			records = append(records, record)
		}
	}
	return records
}
