package unit

// How a direct-style body ends, and what it may not do. The body runs on a
// goroutine of its own, so these are the cases that goroutine makes possible.

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang/effect"
)

func TestAFailedAwaitRunsTheBodysDeferredCalls(t *testing.T) {
	var deferred, after bool
	program := effect.Gen(func(do *effect.Do[effect.Unit, string]) string {
		defer func() { deferred = true }()
		do.Await(directOperations.Fail[string]("refused"))
		after = true
		return "unreachable"
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, _ := exit.Cause()
	if failure, _ := cause.Failure(); failure != "refused" {
		t.Fatalf("unexpected exit: %v", exit)
	}
	if !deferred || after {
		t.Fatalf("deferred=%v after=%v; a failure runs defers and nothing after it", deferred, after)
	}
}

func TestAGoexitOfTheBodysOwnIsADefect(t *testing.T) {
	// testing.T's FailNow inside a body is the realistic way to get here.
	program := effect.Gen(func(*effect.Do[effect.Unit, string]) string {
		runtime.Goexit()
		return "unreachable"
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, _ := exit.Cause()
	if !cause.ContainsDefect() || !strings.Contains(cause.String(), "runtime.Goexit") {
		t.Fatalf("expected a defect naming the Goexit, got %v", exit)
	}
}

func TestInterruptionEndsTheBodyAtItsNextAwait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var after bool
	program := effect.Gen(func(do *effect.Do[effect.Unit, string]) string {
		cancel()
		do.Await(effect.Sleep[effect.Unit, string](time.Hour))
		after = true
		return "unreachable"
	})

	exit := effect.Run(ctx, effect.Unit{}, program)
	cause, _ := exit.Cause()
	if !cause.HasInterruptsOnly() || after {
		t.Fatalf("expected interruption only, got %v (after=%v)", exit, after)
	}
}

func TestAwaitFromAGoroutineTheBodyStartedIsADefect(t *testing.T) {
	program := effect.Gen(func(do *effect.Do[effect.Unit, string]) string {
		release := make(chan struct{})
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			do.Await(directOperations.From(func(context.Context, effect.Unit) effect.Exit[string, string] {
				<-release
				return effect.ExitSuccess[string]("stray")
			}))
		}()
		time.Sleep(10 * time.Millisecond)
		defer func() {
			close(release)
			<-finished
		}()
		return do.Await(directOperations.Succeed("body"))
	})

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	cause, _ := exit.Cause()
	if !cause.ContainsDefect() || !strings.Contains(cause.String(), "two goroutines") {
		t.Fatalf("expected a defect naming the misuse, got %v", exit)
	}
}

func TestOneDirectProgramRunsConcurrently(t *testing.T) {
	var total atomic.Int64
	program := effect.Gen(func(do *effect.Do[effect.Unit, string]) string {
		total.Add(int64(len(do.Await(directOperations.Succeed("x")))))
		return "done"
	})

	var group sync.WaitGroup
	for range 64 {
		group.Go(func() { effect.Run(context.Background(), effect.Unit{}, program) })
	}
	group.Wait()
	if total.Load() != 64 {
		t.Fatalf("expected 64 runs, got %d", total.Load())
	}
}

func TestARetryRunsTheBodyAgain(t *testing.T) {
	attempts := 0
	program := effect.Gen(func(do *effect.Do[effect.Unit, string]) int {
		attempts++
		if attempts < 3 {
			do.Fail("again")
		}
		return attempts
	}).RetryN(5)

	exit := effect.Run(context.Background(), effect.Unit{}, program)
	if value, _ := exit.Value(); value != 3 {
		t.Fatalf("unexpected exit: %v", exit)
	}
}
