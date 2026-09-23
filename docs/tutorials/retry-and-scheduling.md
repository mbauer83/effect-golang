# Retry with a deterministic schedule

Retry policy is data. Construct it independently, then attach it to the one
effect whose typed failures are safe to repeat:

```go
policy := effect.IntersectSchedules(
    effect.Recurs[effect.IOError](3),
    effect.Exponential[effect.IOError](
        100*time.Millisecond,
        5*time.Second,
    ),
)

program := filecopy.ProgramWithRetry("input.txt", "output.txt", policy)
exit := effect.Run(context.Background(), effect.Unit{}, program)
```

The first read is immediate. `Recurs(3)` permits three retries after that first
attempt, while `Exponential` selects delays of 100 ms, 200 ms, and 400 ms. The
intersection continues only while both policies continue and uses the longer
delay from each decision.

`Retry` only repeats an exact typed `Fail(IOError)`. Defects, interruption, and
composite causes are returned without retrying. Use `RetryCause` only when an
all-typed composite cause is intentionally retryable. Exhaustion preserves the
last typed error; `RetryOrElse` can instead evaluate a typed fallback with that
error and the final schedule output.

## Test without wall-clock waiting

Install a manual clock on one runtime:

```go
clock := effecttest.NewManualClock(time.Unix(0, 0))
runtime, err := effect.NewRuntime(effect.WithClock(clock))
if err != nil {
    t.Fatal(err)
}

result := make(chan effect.Exit[effect.IOError, effect.Unit], 1)
go func() {
    result <- runtime.Run(context.Background(), effect.Unit{}, program)
}()

if err := clock.WaitForPending(ctx, 1); err != nil {
    t.Fatal(err)
}
clock.Advance(100 * time.Millisecond)
```

`WaitForPending` is synchronization, not a real-time sleep. The same clock drives
`Now`, `Sleep`, `Delay`, retry, repeat, log timestamps, and span durations.

The end-to-end test in `filecopy_retry_e2e_test.go` combines a scripted
filesystem, manual clock, structured logger, observer, retry policy, and the
complete file-copy program.
