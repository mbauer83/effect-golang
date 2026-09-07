// Package fanout is a complete program showing a pipeline in which every piece
// is load-bearing.
//
// Records are read from a file as a stream, handed to a fixed number of workers
// through a bounded queue, classified, and fanned out to two reporters that
// each want a different summary. A deferred value carries the file's label,
// computed once by the reader and needed by both reporters.
//
// Nothing here sleeps or polls. The queue's shutdown ends the workers, the
// workers ending lets the hub shut down, and the hub's shutdown ends the
// reporters -- so the pipeline drains in order and the program finishes when
// there is genuinely nothing left to do.
package fanout

import (
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mbauer83/effect-golang/effect"
)

// Report is the combined outcome of one run. Both reporters label their own
// summary, and the labels have to agree, because both read the same deferred
// value.
type Report struct {
	Label    string
	Records  int
	Warnings int
}

// warnings is the second reporter's summary.
type warnings struct {
	Label string
	Count int
}

// classified is one record with the verdict a worker gave it.
type classified struct {
	Record  string
	Warning bool
}

type pipeline[A any] = effect.Effect[effect.Unit, effect.IOError, A]

const (
	queueDepth = 8
	chunkSize  = 4
)

var io = effect.IO()

// Program runs the pipeline with the given number of workers.
func Program(inputPath string, workers int) pipeline[Report] {
	return effect.Scoped(func(scope effect.Scope) pipeline[Report] {
		return openPipeline(scope, inputPath, workers).
			Named("fanout").
			WithSpan("fanout", slog.String("input", inputPath), slog.Int("workers", workers))
	})
}

func openPipeline(scope effect.Scope, inputPath string, workers int) pipeline[Report] {
	return effect.Zip(
		io.ScopedQueue[string](scope, queueDepth, effect.SuspendWhenFull),
		io.Hub[classified](scope, queueDepth, effect.SuspendWhenFull),
	).FlatMap(func(shared effect.Product[effect.Queue[string], effect.Hub[classified]]) pipeline[Report] {
		return io.Deferred[string]().FlatMap(func(label effect.Deferred[effect.IOError, string]) pipeline[Report] {
			return run(scope, inputPath, workers, shared.First, shared.Second, label)
		})
	})
}

// run wires the stages. Reporters subscribe before anything is published, so no
// result is delivered into the void.
func run(
	scope effect.Scope,
	inputPath string,
	workers int,
	records effect.Queue[string],
	results effect.Hub[classified],
	label effect.Deferred[effect.IOError, string],
) pipeline[Report] {
	return effect.Zip(
		io.Subscribe(scope, results),
		io.Subscribe(scope, results),
	).FlatMap(func(feeds effect.Product[effect.Subscription[classified], effect.Subscription[classified]]) pipeline[Report] {
		counting := io.Fork(countRecords(feeds.First, label))
		warning := io.Fork(countWarnings(feeds.Second, label))
		return effect.Zip(counting, warning).FlatMap(
			func(reporters effect.Product[effect.Fiber[effect.IOError, Report], effect.Fiber[effect.IOError, warnings]]) pipeline[Report] {
				return produceAndClassify(inputPath, workers, records, results, label).
					AndThen(effect.Zip(
						io.Join(reporters.First),
						io.Join(reporters.Second),
					)).
					Map(combine)
			},
		)
	})
}

func combine(both effect.Product[Report, warnings]) Report {
	counted := both.First
	counted.Warnings = both.Second.Count
	if both.Second.Label != counted.Label {
		// Both reporters read the same Deferred, so disagreeing labels would
		// mean it had been fulfilled twice.
		counted.Label = "inconsistent: " + counted.Label + " / " + both.Second.Label
	}
	return counted
}

// produceAndClassify reads the file into the queue and runs the workers that
// drain it, then shuts the hub down once every worker has finished.
func produceAndClassify(
	inputPath string,
	workers int,
	records effect.Queue[string],
	results effect.Hub[classified],
	label effect.Deferred[effect.IOError, string],
) pipeline[effect.Unit] {
	reading := io.Fork(readInto(inputPath, records, label))
	return reading.FlatMap(func(reader effect.Fiber[effect.IOError, effect.Unit]) pipeline[effect.Unit] {
		return effect.ForEachPar(workerIndices(workers), func(int) pipeline[effect.Unit] {
			return classifyFrom(records, results)
		}).
			AndThen(io.Join(reader)).
			// The same debt one stage further on: once nothing more will be
			// published, the reporters have to be told, whether the workers
			// finished or failed.
			Ensuring(results.Shutdown[effect.Unit]())
	})
}

func workerIndices(workers int) []int {
	if workers < 1 {
		workers = 1
	}
	return make([]int, workers)
}

// readInto supplies the label and then streams the file's records into the
// queue. RunIntoQueue shuts the queue down when the stream ends, which is what
// lets the workers finish.
//
// If the read fails, the reporters are left waiting on a label that will never
// be supplied, and nothing here hands them the failure -- deliberately. Closing
// the enclosing scope cancels them, which is the runtime's job and not this
// program's. Completing the Deferred by hand would be duplicating a guarantee
// the scope already makes.
func readInto(
	inputPath string,
	records effect.Queue[string],
	label effect.Deferred[effect.IOError, string],
) pipeline[effect.Unit] {
	return io.ReadFile(inputPath).
		FlatMap(func(content []byte) pipeline[effect.Unit] {
			lines := splitRecords(content)
			return io.WidenError(label.Succeed[effect.Unit](labelFor(inputPath, lines))).
				AndThen(effect.RunIntoQueue(io.StreamOf(lines...), records))
		}).
		// Whoever fills a queue owes its consumers a shutdown on every outcome,
		// not only on the one where the stream reached its end. Without this, a
		// read that fails before the stream starts leaves every worker blocked
		// on a queue nobody will fill.
		Ensuring(records.Shutdown[effect.Unit]()).
		Named("read-records")
}

func labelFor(inputPath string, records []string) string {
	return filepath.Base(inputPath) + " (" + strconv.Itoa(len(records)) + " records)"
}

func splitRecords(content []byte) []string {
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	records := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			records = append(records, trimmed)
		}
	}
	return records
}

// classifyFrom drains the queue and publishes a verdict for every record. It
// ends when the queue reports that it has been shut down and drained.
func classifyFrom(records effect.Queue[string], results effect.Hub[classified]) pipeline[effect.Unit] {
	return effect.RunForEach(
		io.StreamFromQueue(records, chunkSize),
		func(record string) pipeline[effect.Unit] {
			return io.WidenError(results.Publish[effect.Unit](classify(record))).As(effect.Unit{})
		},
	).Named("classify")
}

func classify(record string) classified {
	return classified{Record: record, Warning: strings.HasPrefix(record, "WARN")}
}

// countRecords labels its report with the deferred value.
//
// Both reporters read that one value, which is exactly what a channel cannot
// do: a channel would give the label to whichever reporter received first and
// leave the other waiting.
func countRecords(
	feed effect.Subscription[classified],
	label effect.Deferred[effect.IOError, string],
) pipeline[Report] {
	return label.Await[effect.Unit]().FlatMap(func(name string) pipeline[Report] {
		return effect.RunFold(
			io.StreamFromSubscription(feed, chunkSize),
			Report{Label: name},
			func(report Report, _ classified) Report {
				report.Records++
				return report
			},
		)
	}).Named("count-records")
}

// countWarnings summarises the same results differently, which is why they go
// to a hub rather than a queue: both reporters see every verdict, and both read
// the same label.
func countWarnings(
	feed effect.Subscription[classified],
	label effect.Deferred[effect.IOError, string],
) pipeline[warnings] {
	return label.Await[effect.Unit]().FlatMap(func(name string) pipeline[warnings] {
		return effect.RunFold(
			io.StreamFromSubscription(feed, chunkSize),
			warnings{Label: name},
			func(counted warnings, verdict classified) warnings {
				if verdict.Warning {
					counted.Count++
				}
				return counted
			},
		)
	}).Named("count-warnings")
}
