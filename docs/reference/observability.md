# Observability reference

Install an observer per runtime:

```go
observer := &effecttest.RecordingObserver{}
runtime, err := effect.NewRuntime(effect.WithObserver(observer))
```

Effects expose three observation boundaries:

- `Named(name)` supplies a stable operation name to nested records and events.
- `Annotate(attrs...)` supplies ordered `slog.Attr` metadata.
- `WithSpan(name, attrs...)` emits start/end events and correlates nested work.

Logging uses the same metadata:

```go
program := operations.LogInfo("loaded", slog.Int("count", count)).
    Named("load-catalog").
    Annotate(slog.String("component", "catalog")).
    WithSpan("catalog-load")
```

Metadata composes rather than replacing: `Annotate` appends to what an enclosing
scope already supplied, and a log call's own fields follow the inherited ones.
Field order is therefore stable and deterministic, which is what makes recorded
records comparable structurally in a test. A key supplied at two levels appears
twice, in that order; a renderer that must deduplicate should take the last
occurrence, and an effect should not repeat a key its enclosing span already
annotates.

`WithSpan` also records its call site once, when the span is described rather
than each time it runs. Source locations are captured at named boundaries only;
`runtime.Caller` on every combinator would be too expensive and too noisy.

## Event vocabulary

| Kind | Boundary |
|---|---|
| `fiber_started`, `fiber_completed` | every fiber, forked explicitly or by a combinator |
| `scope_opened`, `scope_closing`, `scope_closed` | a lifetime |
| `resource_acquired`, `resource_released` | a scoped resource |
| `span_started`, `span_ended` | `WithSpan` |
| `retry_scheduled`, `retry_exhausted`, `retry_succeeded` | retry |
| `repeat_scheduled`, `repeat_completed` | repetition |
| `log_emitted` | a delivered log record |
| `runtime_closing`, `runtime_closed` | `Runtime.Close` |

Events carry stable fiber, parent-fiber, span and parent-span identities,
monotonic timestamps, durations, an attempt number, a delay, a bounded
`EventStatus` and ordered attributes. They never carry an environment, a failure
value or a successful result, so an observer cannot become a channel for
sensitive data.

`EventStatus` is the whole terminal vocabulary: `success`, `typed_failure`,
`defect`, `interrupted`. A defect outranks an interruption.

Observers run inline. An adapter that blocks or exports remotely must own and
document its queue, shutdown, and overflow behavior. A panic from an observer is
contained and cannot change the observed effect's `Exit`.

## Metrics

The core defines events and measurements, not a metrics backend. Fiber
duration, scope close duration, retry counts, retry delay, defects and
interruptions are all derivable from the fields above.

Never use a message, path, error string or arbitrary annotation value as a
metric label. An export adapter must enforce a bounded label vocabulary; the
`EventStatus` and `EventKind` values are bounded by construction, and
`Operation` is bounded only if the application keeps it so.

## Diagnostics

Instrumentation can fail, and that failure is not an application outcome. A
`Logger` that cannot deliver a record and an `Observer` that panics are both
reported to the runtime `Diagnostics` sink and never widen `E` or become a
defect:

```go
diagnostics := &effecttest.RecordingDiagnostics{}
runtime, err := effect.NewRuntime(effect.WithDiagnostics(diagnostics))
```

The live default writes one line per fault to standard error, deliberately not
through the configured `Logger`, because a `Logger` fault is one of the things
it must be able to report.

## Debug tracking

`WithDebugTracking()` makes a runtime count the fibers and scoped resources it
owns. `Runtime.LiveWork()` reports the current snapshot, which is what a test
asserts on, and `Close` reports any remainder to diagnostics. It costs a pair of
atomic counters per fiber and per resource, which is why it is not the default.

```go
runtime, _ := effect.NewRuntime(effect.WithDebugTracking())
runtime.Run(ctx, env, program)

if live := runtime.LiveWork(); !live.IsEmpty() {
    t.Fatalf("program left work behind: %#v", live)
}
```

## Integration boundary

The base package owns `LogRecord`, `Logger`, the span and event data model, the
observer hook, cause rendering and the capability interfaces. OpenTelemetry,
`slog` adapters and vendor exporters belong in optional integration packages, so
the runtime is observable without a required dependency and without tying its
semantics to one telemetry ecosystem. An adapter may derive a conventional
context value at the external API boundary; the runtime keeps its own explicit
representation and never smuggles metadata through `context.Value`.

## Rendering

`Cause.String()` gives a stable human-readable tree; `%+v` additionally includes
captured defect stacks. `Cause.Report()` gives the same tree structurally, for
a log or an exporter. Both are iterative and total.

No question about an outcome needs display text to answer it: `Failures`,
`Defects`, `Interruptions`, `Status` and the predicates report the same facts
directly, so tests and tools never parse rendered output.
