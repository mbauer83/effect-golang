# Core reference

## Algebra

`Product[A,B]` is an ordinary product with `First` and `Second` fields.

`Either[L,R]` is an exclusive sum. It is right-biased: `Map` and `FlatMap` operate on `R`. The discriminator and payloads are private; use `Left`, `Right`, `LeftValue`, `RightValue`, or `Fold`.

## Effect

`Effect[R,E,A]` is suspended until `Run`.

Key operations:

- `Map`: transform `A`.
- `MapError`: transform expected `E`.
- `ContramapEnv`: adapt `R`.
- `Provide`: supply all of `R`.
- `FlatMap`: sequence effects with the same `R` and `E`.
- `FlatMapChannels(fx, f)`: sequence effects with different channels using `Product` and `Either`.
- `Zip(left, right)` / `ZipChannels(left, right)`: retain both successful values.
- `CatchAll`: handle a leaf typed `E`; defects and interruption remain unhandled.
- `As`, `Tap`, `AndThen`, and `Flatten`: common sequential shapes without a
  nested callback.
- `Suspend(create)`: defer construction to interpretation, so a description can
  allocate its own per-run state.
- `ExitOf(fx)` and `Fold(fx, failure, success)`: eliminate an outcome without
  leaving the effect world. Both are package functions because their result is
  built from the effect's own channels.
- `WidenError[E](fx)`: retype an `Effect[R, Never, A]` so it composes with
  failing work. Free and total, because `Never` is uninhabited.
- `OrDie(fx)`: rewrite typed failures as defects for use where the
  failure channel must be `Never`.
- `All`, `ForEach`: evaluate a collection in order, short-circuiting.

`Operations[R,E]`, constructed with `For[R,E]()`, lets base clock and logging
effects inherit phantom channels from one declaration. `IO()` and `IOFor[R]()`
add filesystem effects with `IOError` as the typed-error channel.

`Operations[R,E]` also carries the pre-widened forms of every operation whose
own failure channel is `Never` or whose requirement channel is unused: `Fork`,
`ForkIn`, `ForkDaemon`, `Await`, `Join`, `Interrupt`, `Send`, `Recv`,
`RecvOrFail`, `Suspend`, `CheckInterrupt`, `WidenError` and
`OrDie`.

`Workflow[R,E,S]` is a small, typed `Do`/`Bind`/`Yield` convenience for longer
dependent sequences. Its state factory runs once per interpretation.

## Lifetimes and concurrency

`Scope` owns fibers and resources through one mechanism. `Scoped(use)` opens
one; `scope.AcquireRelease` registers a resource; `scope.Fork`, `Fork` and
`ForkDaemon` choose an owner. `fx.Ensuring` and `fx.OnExit` attach cleanup to a
single effect. See [scope](scope.md) and [fiber](fiber.md).

`ZipPar`, `ZipParChannels`, `Race`, `RaceFirst`, `ForEachPar`, `ForEachParN`,
`AllPar`, `AllParN`, `Timeout`, `TimeoutFail` and `TimeoutTo` are the
structured-concurrency operators. None returns while work it discarded is still
running or finalizing.

`Send`, `Recv` and `RecvOrFail` adapt ordinary Go channels; see
[channels](channels.md).

## Runtime

`NewRuntime(options...)` builds an interpreter with live defaults and
runtime-local overrides: `WithClock`, `WithFileSystem`, `WithLogger`,
`WithObserver`, `WithDiagnostics`, `WithDebugTracking`. A nil capability is
rejected at construction. `Runtime.Run` interprets in a lifetime of its own;
`Runtime.Close` ends the root lifetime, awaits detached work, flushes buffering
capabilities and reports the composed cleanup cause as a `Cause[Never]`.

The package-level `Run` uses an ephemeral runtime whose root is closed before it
returns.

## Retry and scheduling

`Schedule[In,Out]` is immutable and creates a fresh driver per interpretation.
The initial policies are `Stop`, `Forever`, `Recurs`, `Spaced`, `Exponential`,
`Fibonacci`, and `UpTo`. `IntersectSchedules` intersects policies and `UnionSchedules`
unions them; `WhileInput`, `WhileOutput`, and `MapOutput` refine a policy.

- `Retry` retries exact typed-failure leaves only.
- `RetryCause` explicitly opts into all-typed composite causes.
- `RetryN(n)` means at most `n` retries after the initial attempt.
- `Repeat` schedules successful values and returns the schedule's final output.
- `Sleep` and `Delay` use the runtime's interruptible clock.

## Exit and Cause

`Run` returns `Exit[E,A]`.

A failed exit contains one compositional `Cause[E]`:

- `CauseEmpty`: composition identity and zero value;
- `CauseFailure`: expected typed `E`;
- `CauseDefect`: panic captured with a stack;
- `CauseInterrupted`: context cancellation or deadline with its cause;
- `CauseThen`: sequential failure composition;
- `CauseBoth`: parallel failure composition.

`Failures`, `Defects`, and `Interruptions` collect leaves without discarding the
tree. `Fold` performs typed elimination, `Status` reduces a cause to a bounded
classification, `String` renders a stable tree and `Report` produces a
structured `CauseReport` for exporters. See [cause](cause.md) and
[interruption](interruption.md).

## Layer

`Layer[RIn,E,ROut]` is effectful construction of an environment.

- `ThenLayers` feeds one layer's output into another.
- `ZipLayers` builds independent layers and retains both outputs.
- `ProvideLayer(fx, layer)` satisfies an effect's `R` and keeps layer/effect failures distinguishable.
- `ProvideLayerSame` avoids a redundant `Either[E,E]` when both use the same error channel.
