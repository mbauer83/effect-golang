# How to test time

Every wait goes through the runtime clock, so a test never sleeps against the
wall clock.

## Install a manual clock

```go
clock := effecttest.NewManualClock(time.Unix(0, 0))
runtime, err := effect.NewRuntime(effect.WithClock(clock))
```

The override belongs to this `Runtime` only. Parallel tests with different
clocks cannot interfere, because there is no global setter to race on.

## Drive a delay

Run the program on another goroutine, wait until it is actually parked, then
advance:

```go
result := make(chan effect.Exit[AppError, Report], 1)
go func() {
    result <- runtime.Run(context.Background(), env, program)
}()

if err := clock.WaitForPending(ctx, 1); err != nil {
    t.Fatal(err)
}
clock.Advance(time.Second)

exit := <-result
```

`WaitForPending` is what makes this deterministic rather than a sleep-and-hope:
it blocks until the expected number of waits have registered.

## Assert an exact schedule

```go
clock.Advance(time.Second)      // first backoff
clock.Advance(2 * time.Second)  // second, doubled
clock.Advance(2 * time.Second)  // third, capped

if got := clock.Now(); !got.Equal(time.Unix(5, 0)) {
    t.Fatalf("expected capped delays totalling five seconds, got %v", got)
}
```

## Drive a timeout

A timeout is a race against the clock, so it is driven the same way:

```go
work.awaitStart()
clock.WaitForPending(ctx, 1)
clock.Advance(2 * time.Second)
```

## Prove concurrency instead of assuming it

Advancing a clock does not show that two branches ran at once. A barrier does,
because a sequential implementation deadlocks on it instead of quietly passing:

```go
func (meeting *barrier) arrive() {
    meeting.participants.Done()
    meeting.participants.Wait()
}
```

## Observe how many branches are parked

`PendingSleeps` reports registered waits, which is how a test can show that a
bounded traversal really bounds:

```go
overBound, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
defer cancel()
if err := clock.WaitForPending(overBound, limit+1); err == nil {
    t.Fatalf("expected at most %d branches in flight", limit)
}
```

## The other deterministic capabilities

| Adapter | Use |
|---|---|
| `effecttest.ManualClock` | advance time by hand |
| `effecttest.LogRecorder` | assert structured records |
| `effecttest.EventRecorder` | assert lifecycle events |
| `effecttest.DiagnosticsRecorder` | assert instrumentation faults |
| `effecttest.FileSystemStub` | inject filesystem behaviour per operation |
| `effecttest.Tracker` | assert ordered lifecycle events across goroutines |

For real files, prefer `t.TempDir()` over an in-memory filesystem that would
have to pretend to reproduce every permission, symlink and rename behaviour.
