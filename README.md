# effect-golang

A small, typed effect system and structured-concurrency runtime for Go 1.27+.

```go
Effect[R, E, A]
```

means: a suspended computation that requires `R`, may fail with an expected `E`,
and may succeed with `A`.

The core design preserves all three channels during composition:

- requirements compose as `Product[R1, R2]`;
- expected failures compose as `Either[E1, E2]`;
- values remain fully typed, using `Product[A, B]` where both are retained.

Panics and cancellation are not smuggled into `E`. `Run` returns `Exit[E, A]`,
whose failure side is a compositional `Cause[E]` that keeps typed failures,
defects and interruption apart and keeps all of them when several things go
wrong at once.

Go 1.27 is required because the public API relies on generic methods.

## What the runtime provides

- **Stack-safe interpretation.** The private evaluator is an instruction
  interpreter with a heap-allocated continuation stack, so a million sequential
  operations, a million retry attempts and an arbitrarily deep cause tree are
  all bounded by memory rather than by goroutine stack depth.
- **Lifetimes.** `Scope` owns fibers and resources through one mechanism.
  Closing it cancels its children, waits for them, then releases resources in
  reverse acquisition order.
- **Fibers.** `Fork`, `Await`, `Join`, `Interrupt`, `Poll` and a `Done()`
  channel for ordinary `select`. Observation is repeatable and multi-consumer.
- **Resource safety.** A successfully acquired resource is released exactly
  once, including when acquisition races scope closure and when the caller is
  already canceled.
- **Structured parallelism.** `ZipPar`, `Race`, `RaceFirst`, `Timeout`,
  `ForEachPar` and a bounded `ForEachParN`. None returns while work it
  discarded is still running or finalizing.
- **Native channels.** `Send`, `Recv` and `RecvOrFail` over ordinary Go
  channels, with Go's own closure and ownership semantics intact.
- **Queue, Hub and Deferred.** Only where a channel genuinely cannot serve:
  shutdown that is safe from any side, a required choice of what a full queue
  does, batched taking, broadcasting to a changing set of subscribers, and a
  value every waiter observes rather than the first receiver consuming.
- **Schedules.** Immutable, concurrently reusable policies with a fresh driver
  per run: recurrence, spacing, capped exponential and Fibonacci backoff,
  elapsed limits, predicates, injected jitter, intersection and union.
- **Retry and repetition.** Typed failures only; never defects, never
  interruption.
- **Capabilities.** Runtime-local `Clock`, `FileSystem`, `Logger`, `Observer`
  and `Diagnostics`, with live defaults and deterministic test adapters. No
  global setters, so parallel tests cannot race on configuration.
- **Observability.** Spans, names, annotations, a bounded lifecycle event model,
  structured cause reports and an optional debug ledger for leak assertions.

Goroutines remain the execution primitive, the Go scheduler remains the
scheduler, and `context.Context` remains the cancellation boundary. There is no
custom scheduler and no CPS runtime.

## Layout

```text
effect/                     package effect -- the public domain: the algebra,
                            Cause, Exit, Scope, Fiber, schedules, retry, the
                            base capability effects and the Runtime facade
  capability/               the ports: Clock, FileSystem, Logger, Observer,
                            Diagnostics
  internal/outcome/         how a terminated effect is modelled
  internal/lifetime/        what owns work: scopes, fibers, cancellation reasons
  internal/runtime/         interpretation: instructions, the trampoline, state
  internal/platform/        live adapters over os, time and log/slog
effecttest/                 deterministic capabilities and fixtures for tests
examples/<scenario>/        runnable programs, one per scenario
test/unit/                  behaviour of the public API
test/acceptance/            the acceptance program and the example scenarios
test/architecture/          invariants over the shape of the code
docs/                       tutorials, how-to guides, reference, explanation
```

```go
import "github.com/mbauer83/effect-golang/effect"
```

The module root holds no source. The dependency rule points inward: `capability`
depends on nothing, `outcome` models a result, `lifetime` owns work, `runtime`
interprets, and the adapters sit outside all of it. Nesting the internals under
`effect/` means Go itself forbids anything outside the domain from reaching
them.

`test/architecture` asserts what Go cannot: the ordering among the internal
layers, that only the composition root names a live adapter, that erased values
stay inside their one documented file, the source-size limits, that the root
stays free of source, and that this index reaches every document.

`Effect`, `Cause`, `Exit`, `Scope` and `Fiber` share private representation, and
Go keeps one package in one directory, so the domain is one directory by
construction. That directory does not have to be the module root, and it is not.

## Documentation

### Tutorials

- [Your first effect](docs/tutorials/first-effect.md)
- [Concurrency](docs/tutorials/concurrency.md)
- [Resources](docs/tutorials/resources.md)
- [Retry and scheduling](docs/tutorials/retry-and-scheduling.md)

### How-to

- [Write sequential workflows](docs/how-to/write-sequential-workflows.md)
- [Compose different channels](docs/how-to/compose-channels.md)
- [Fork and join](docs/how-to/fork-and-join.md)
- [Run parallel work](docs/how-to/run-parallel-work.md)
- [Cancel work](docs/how-to/cancel-work.md)
- [Manage resources](docs/how-to/manage-resources.md)
- [Use Go channels](docs/how-to/use-go-channels.md)
- [Adapt context-aware Go APIs](docs/how-to/adapt-context-apis.md)
- [Retry failures](docs/how-to/retry-failures.md)
- [Test time](docs/how-to/test-time.md)

### Reference

- [Core model](docs/reference/core.md)
- [Scope](docs/reference/scope.md)
- [Fiber](docs/reference/fiber.md)
- [Cause](docs/reference/cause.md)
- [Interruption](docs/reference/interruption.md)
- [Channels](docs/reference/channels.md)
- [Queue](docs/reference/queue.md)
- [Hub](docs/reference/hub.md)
- [Deferred](docs/reference/deferred.md)
- [Schedule](docs/reference/schedule.md)
- [Retry](docs/reference/retry.md)
- [Observability](docs/reference/observability.md)

### Explanation

- [Design and trade-offs](docs/explanation/design.md)
- [The Go runtime model, and where this library differs](docs/explanation/go-runtime-model.md)
- [Structured concurrency](docs/explanation/structured-concurrency.md)
- [Resource safety](docs/explanation/resource-safety.md)
- [Why a cause algebra](docs/explanation/cause-algebra.md)
- [Sequencing in Go](docs/explanation/sequencing-in-go.md)

## Examples

Every scenario is a complete program using only the public API, and every one is
composed again by an end-to-end test, so the examples and their guarantees
cannot drift apart.

| Scenario | Exercises |
|---|---|
| [`filecopy`](examples/filecopy/program.go) | filesystem, clock, schedules, retry, structured logging, spans |
| [`parallelimport`](examples/parallelimport/program.go) | scope, acquire/release, bounded parallel traversal, cancellation |
| [`diagnostics`](examples/diagnostics/program.go) | typed failure, panic defect, `Then`/`Both`, cause rendering |
| [`checkout`](examples/checkout/program.go) | dependent sequential workflow, typed state builder |
| [`pipeline`](examples/pipeline/program.go) | `Send`, `Recv`, fiber `Done`, producer-owned closure |

`go run ./examples/cmd/effectdemo` runs all five against live capabilities.

## Scope of the project

The project is intentionally focused on the core algebra and runtime semantics
first. Ecosystem integrations — OpenTelemetry, `slog` adapters, vendor
exporters — belong in optional packages and will be evaluated separately for
adoption, usability, security, documentation, maintenance and long-term
compatibility.

An effect-specific abstraction is introduced only where it adds semantics native
Go channels genuinely lack. `Queue`, `Hub` and `Deferred` clear that bar and are
present. `Stream` does too and is not built yet.
