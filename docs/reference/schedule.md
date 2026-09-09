# Schedule reference

A schedule decides **whether and when to evaluate again**. The Go runtime still
schedules every goroutine: there is no timer wheel, worker thread or background
effect scheduler.

## Values and drivers

```text
Schedule value    immutable and reusable
    | start
driver            fresh mutable state for one interpretation
    | input + clock time
ScheduleDecision  continue or stop, a delay, and an observable output
```

State such as an attempt count, elapsed time or the previous Fibonacci delay
belongs to a driver, never to the shared `Schedule`. One `Schedule` value is
therefore safe to reuse concurrently.

`In` makes the same machinery serve both retry (input is a failure) and
repetition (input is a successful value). `Out` exposes useful policy state
without weakening `E`.

`ScheduleDecision[Out]` reports `Output()`, `Delay()` and `Continues()`.

## Policies

| Constructor | Meaning |
|---|---|
| `Stop` | never recurs |
| `Forever` | recurs immediately, output counts from zero |
| `Recurs(n)` | at most `n` continuations after the first execution |
| `Spaced(d)` | waits `d` after completion before starting again |
| `Exponential(base, cap)` | doubling backoff, saturating at `cap` |
| `Fibonacci(one, cap)` | Fibonacci backoff, saturating at `cap` |
| `UpTo(limit)` | continues while elapsed clock time is below `limit` |

## Refinement and composition

- `MapOutput(f)` transforms output without sharing driver state.
- `WhileInput(p)` continues only while `p` accepts the latest input.
- `WhileOutput(p)` continues only while `p` accepts the latest output.
- `Jittered(random, min, max)` scales each continuing delay by an injected
  random fraction. Randomness is injected so tests stay deterministic; no
  package-global random state is used. Fractions outside `[0,1]` are clamped and
  `NaN` selects the minimum.
- `IntersectSchedules(left, right)` continues only while both continue and waits
  `max(left, right)`. Output is `Product`.
- `UnionSchedules(left, right)` continues while either continues and waits the
  shortest active delay. Output is `ScheduleUnion`, which reports which
  component asked to continue.

## Timing rules

- The first attempt or run is immediate.
- A retry delay is measured after the failed attempt completes.
- Negative delays normalize to zero.
- Duration arithmetic saturates instead of overflowing.
- Elapsed time uses the injected clock; the live clock uses Go's monotonic
  component.
- A canceled clock wait produces `Interrupt` and preserves
  `context.Cause(ctx)`.

## Deliberate omissions

Calendar and cron scheduling, persistence across restarts and distributed job
leasing need wall-clock and ownership semantics unlike retry delays, and are
outside the core runtime. Schedules are pure and infallible apart from a defect
raised by a panicking user callback; effectful policies would turn policy
evaluation into another effect problem.

`FixedRate` is deliberately absent rather than conflated with `Spaced`: it needs
planned start times, no overlap and skipped missed ticks.
