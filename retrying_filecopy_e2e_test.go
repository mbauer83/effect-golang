package effect_test

import (
	"context"
	"errors"
	"io/fs"
	"sync/atomic"
	"testing"
	"time"

	effect "github.com/mbauer83/effect-golang"
	"github.com/mbauer83/effect-golang/effecttest"
	"github.com/mbauer83/effect-golang/examples/filecopy"
)

func TestRetryingFileCopyProgramRecoversFromTransientReadFailures(t *testing.T) {
	var reads atomic.Int32
	written := make(chan []byte, 1)
	fileSystem := effecttest.FileSystemStub{
		ReadFileFunc: func(context.Context, string) ([]byte, error) {
			if reads.Add(1) <= 2 {
				return nil, errors.New("transient read")
			}
			return []byte("eventually available"), nil
		},
		WriteFileFunc: func(_ context.Context, _ string, data []byte, permissions fs.FileMode) error {
			if permissions != 0o600 {
				return errors.New("unexpected permissions")
			}
			written <- append([]byte(nil), data...)
			return nil
		},
	}
	clock := effecttest.NewManualClock(time.Unix(0, 0))
	logger := &effecttest.RecordingLogger{}
	observer := &effecttest.RecordingObserver{}
	runtime, err := effect.NewRuntime(
		effect.WithFileSystem(fileSystem),
		effect.WithClock(clock),
		effect.WithLogger(logger),
		effect.WithObserver(observer),
	)
	if err != nil {
		t.Fatal(err)
	}

	policy := effect.AndSchedules(
		effect.Recurs[effect.IOError](2),
		effect.Spaced[effect.IOError](time.Second),
	)
	result := make(chan effect.Exit[effect.IOError, effect.Unit], 1)
	go func() {
		result <- runtime.Run(
			context.Background(),
			effect.Unit{},
			filecopy.RetryingProgram("in", "out", policy),
		)
	}()

	clock.AwaitSleepers(t, 1)
	clock.Advance(time.Second)
	clock.AwaitSleepers(t, 1)
	clock.Advance(time.Second)

	exit := <-result
	if !exit.IsSuccess() || reads.Load() != 3 {
		t.Fatalf("unexpected retrying copy result: exit=%+v reads=%d", exit, reads.Load())
	}
	if output := string(<-written); output != "EVENTUALLY AVAILABLE" {
		t.Fatalf("unexpected written data: %q", output)
	}
	if len(logger.Records()) != 1 {
		t.Fatalf("expected one successful operation log, got %#v", logger.Records())
	}

	var scheduled int
	var succeeded int
	for _, event := range observer.Events() {
		switch event.Kind {
		case effect.EventRetryScheduled:
			scheduled++
		case effect.EventRetrySucceeded:
			succeeded++
		}
	}
	if scheduled != 2 || succeeded != 1 {
		t.Fatalf("unexpected retry events: %#v", observer.Events())
	}
}
