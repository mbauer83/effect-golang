# Effect-Golang Runtime Architecture and Implementation Plan

## Status and objective

The existing core establishes the basic typed effect model:

```go
Effect[R, E, A]
```

with:

- lazy execution;
- typed requirements `R`;
- typed expected failures `E`;
- typed success `A`;
- `Either` and `Product` for exact structural composition;
- `Exit[E, A]`;
- a compositional `Cause[E]` with stable rendering and folds;
- `Layer[RIn, E, ROut]`.

The first implementation slice now also provides runtime-local Clock,
FileSystem, Logger and Observer ports with live/test adapters; immutable
retry/repeat schedules and deterministic drivers; structured span/log/retry
events; channel-carrying `Operations`; a small typed `Workflow`; and executable
file-copy scenarios. Scope, Fiber, resource finalization, high-level parallelism,
runtime shutdown and the stack-safe instruction interpreter remain planned work.

The next stage should turn this from a typed description/composition library into a genuine **resource-safe, structured concurrent effect runtime**.

The implementation should remain recognizably Go:

- goroutines are the execution primitive;
- the Go scheduler remains the scheduler;
- `context.Context` remains the cancellation/deadline interoperability mechanism;
- channels and `select` remain the primary communication/waiting primitives;
- retry and repetition are interpreted as ordinary sequential effect execution plus
  interruptible clock waits, not as jobs submitted to a second scheduler;
- mutexes are used where shared-state synchronization is simpler and more correct than message passing;
- no CPS runtime, green-thread scheduler, or bespoke async executor is introduced.

Go explicitly supports channels as concurrent communication primitives, including safe concurrent use, blocking communication and `select`; `context` provides cancellation propagation and cancellation causes; and Go 1.27's `sync.WaitGroup.Go` is now the preferred standard-library mechanism for tracking spawned tasks.

The target should be comparable in guarantees to the useful parts of ZIO/Effect structured concurrency while differing where Go already provides a better native mechanism.

## Research baseline

The design review used pinned upstream source, not analogy from memory:

- [ZIO at `682eead`](https://github.com/zio/zio/tree/682eead00bda9904be94413c145abe4287128f72), especially `Schedule`, `ZIO.retryOrElseEither`, scoped resources, fibers and test clocks;
- [Effect at `5a80204`](https://github.com/Effect-TS/effect/tree/5a802043984727b0c5a291af39d1b9bbfa8d7b8b), especially the schedule driver, retry/repeat interpreter, `Effect.gen`, record-based `Do`/`bind`, scopes and runtime services;
- the released [Go language specification](https://go.dev/ref/spec), standard-library cancellation/timer behavior and compiler issue [`golang/go#80172`](https://github.com/golang/go/issues/80172).

Revisit these sources for each runtime topic, but translate invariants rather than
surface syntax. Scala variance and language comprehensions, TypeScript structural
record growth and generators, and either runtime's custom fiber scheduler are not
directly portable to Go.

---

# 1. Core runtime principles

## 1.1 Do not build a scheduler

An effect fiber should map approximately to:

```text
Effect fiber    -> goroutine
suspension      -> goroutine parked by Go
completion      -> closed channel
interruption    -> context cancellation
waiting/racing  -> select
structured life -> Scope
```

A channel receive, timer, network operation accepting a context, or `select` already allows the Go runtime to park a goroutine without tying up an OS thread.

Consequently, `Sleep`, `Join`, `Await`, channel receive, queue waits and similar operations should simply block/`select` in the ordinary Go manner.

This is an important architectural boundary:

> The effect runtime owns **semantics and lifetimes**, not CPU scheduling.

ZIO needs a fiber scheduler because it is implementing lightweight fibers over the JVM. Go already has goroutines and a runtime scheduler.

---

## 1.2 Cancellation remains cooperative

We cannot truthfully promise ZIO-style interruption of arbitrary Go code.

An effect implemented as:

```go
From(func(ctx context.Context, env R) Exit[E, A] {
    for {
        // never observes ctx
    }
})
```

cannot safely be forcibly stopped by our library.

The runtime should instead guarantee:

1. built-in combinators observe cancellation;
2. channel and timer effects select on `ctx.Done()`;
3. adapters propagate contexts into context-aware Go APIs;
4. cancellation is checked between sequential effect stages;
5. arbitrary user callbacks are documented as responsible for cooperating with cancellation.

This matches Go's context model: cancellation is a signal that work performed on behalf of a context should stop.

We should provide an explicit:

```go
CheckInterrupt()
```

for CPU-heavy/custom loops that need convenient cooperative checkpoints.

---

## 1.3 Respect Go's generic instantiation-cycle boundary

Go 1.27 generic methods improve fluent APIs, but the 1.27.1 compiler rejects a
method returning its receiver's generic type with a type argument constructed
from a receiver argument. For example:

```go
// Rejected when instantiated: A grows to Product[A, B], then could grow again.
func (fx Effect[R, E, A]) Zip[B any](that Effect[R, E, B])
    Effect[R, E, Product[A, B]]
```

The equivalent package-level generic function compiles. This is tracked by the
Go project as issue `golang/go#80172`; the architecture must target the released
compiler rather than assume a future relaxation.

Use this stable rule:

```text
shape-preserving or fresh-result transform -> fluent generic method
type argument constructed from receiver    -> package-level generic function
```

Therefore `Map`, `FlatMap`, `MapError`, `Retry` and similar transforms can remain
methods, while `Zip`, `ZipPar`, `Fork`, heterogeneous channel merges and
type-growing schedule combinations are functions. Do not hide this distinction
behind code generation or `any`; document it consistently so the API remains
predictable.

---

## 1.4 Package topology follows hexagonal dependency direction

Do not introduce a `src/` directory. It is not the normal Go module convention,
would leak `src` into import paths, and would not by itself enforce an
architectural boundary.

Use this topology as the implementation grows:

```text
effect/                            public domain algebra and narrow facade
effect/capability/                 the ports the core defines
effect/internal/outcome/           causes and exits: how a result is modelled
effect/internal/lifetime/          scopes, fibers, cancellation reasons
effect/internal/runtime/           interpretation and runtime state
effect/internal/platform/          live clock/filesystem/logger adapters
effecttest/                        public deterministic test capabilities
examples/<scenario>/               runnable, end-to-end-tested programs
test/unit/                         behaviour of the public API
test/acceptance/                   acceptance program and example scenarios
test/architecture/                 invariants over the shape of the code
integration/<ecosystem>/           optional slog/OpenTelemetry/etc. adapters
```

CORRECTED: the domain package is a directory, not the module root. The
prohibition on `src/` stands and is unrelated: `src` would put a segment
conveying nothing into every import path, whereas `effect/` names what it
contains and yields `github.com/mbauer83/effect-golang/effect`. The module root
holds only project metadata and documentation.

ADDED: nesting the internals under `effect/` makes Go enforce the outer boundary
directly -- nothing outside the domain can import them at all -- leaving
`test/architecture` to assert only the ordering among the internal layers, which
Go cannot express.

CORRECTED: the single `internal/runtime` package above was split once its
contents had three distinct owners. `outcome` models a terminated effect,
`lifetime` owns work, and `runtime` interprets; the layering is acyclic in that
order and is asserted rather than assumed.

ADDED: Go keeps one package in one directory, and `Effect`, `Exit`, `Cause`,
`Scope` and `Fiber` share private representation, so the public domain is
necessarily one directory. What can be separated is the machinery beneath it and
the tests above it, and both are. Exactly one production-package test remains at
the root, because schedule driver laws are stated over the private driver.

ADDED: exactly one file in the domain package may name a live adapter. That file
is the composition root, and a test enforces it, so the domain cannot acquire an
infrastructure dependency one convenience at a time.

The dependency rule points inward:

```text
platform/integrations -> runtime ports -> domain algebra
examples/applications -> public effect API
domain algebra        -X-> platform or exporters
```

The module-root package is the aggregate boundary for the public effect model:
`Effect`, `Exit`, `Cause`, structural channel types and their laws. Runtime
aggregate roots own coherent state transitions for a Runtime, Scope, Fiber or
Schedule driver. Live adapters implement narrow ports defined toward the core;
the core never imports OS/exporter implementations.

Keep files cohesive and below 350 lines (250-line soft limit). Split by domain
role, not by mechanical suffix: cause algebra, cause rendering, effect
construction, sequential composition, runtime capabilities and live adapters are
separate units. Avoid tiny forwarding packages and interfaces with only one
implementation unless the interface is a real hexagonal port or test seam.

## 1.5 Work in expansion and consolidation cycles

Every bounded feature expansion must be followed by a consolidation pass before
the next expansion. The pass re-reads the project standards and checks:

```text
domain and aggregate ownership
hexagonal dependency direction
existing or near-existing abstractions with the same role
cross-domain concepts leaking into generic runtime components
duplicate behavior and inconsistent names
boolean mode flags and overly wide signatures
premature indirection or an abstraction fighting Go
source size, cognitive complexity and nesting
avoidable top types, casts or QA suppressions
tests, examples, docs and generated-code drift
```

Consolidation may remove a new abstraction. New code is not evidence that the
abstraction deserves to survive. Conversely, repeated mechanics should only be
factored after their shared domain rule is understood; textual similarity alone
is not a domain boundary.

---

# 2. Replace the current scalar `Cause` with a compositional cause algebra

The existing:

```text
Failure(E)
Defect
Interrupted
```

is sufficient only while execution is sequential.

It becomes lossy as soon as:

- both parallel branches fail;
- an effect fails and its finalizer also defects;
- several finalizers defect;
- nested concurrent scopes fail in different ways.

ZIO's `Cause` exists specifically to preserve this complete failure history, including sequential and parallel failures.

## Proposed algebra

```text
Cause[E] =
    Empty
  | Fail(E)
  | Die(Defect)
  | Interrupt(Interruption)
  | Then(Cause[E], Cause[E])
  | Both(Cause[E], Cause[E])
```

`Then` means causal/sequential composition:

```text
operation failed
      THEN
cleanup failed
```

`Both` means independent concurrent composition:

```text
left failed  AND  right failed
```

`Empty` makes the zero value meaningful and gives composition an identity.

A possible internal representation remains an opaque tagged structure rather than exposing a forest of variant structs:

```go
type Cause[E any] struct {
    kind causeKind

    failure E
    defect  Defect

    interruption Interruption

    left  *Cause[E]
    right *Cause[E]
}
```

Public construction should remain controlled so invalid states cannot normally be created.

### Required operations

At minimum:

```go
Kind()
MapFailure(...)
Fold(...)
Failures()
Defects()
Interruptions()

IsEmpty()
IsInterruptedOnly()

Then(...)
Both(...)
```

`Exit` continues to be:

```go
Either[Cause[E], A]
```

conceptually.

### Interruption

Do not store only:

```go
ctx.Err()
```

because that throws away cancellation causes.

Use:

```go
context.Cause(ctx)
```

where available. `WithCancelCause` exists precisely to distinguish the underlying cancellation reason from generic `context.Canceled`.

An interruption should therefore contain something like:

```go
type Interruption struct {
    Cause error
}
```

with optional `FiberID` provenance later if it proves useful for diagnostics.

---

# 3. Introduce `Scope` as the fundamental lifetime abstraction

`Scope` should govern **both resource lifetimes and structured child concurrency**.

Conceptually:

```text
Scope
├── child fiber
│   └── its child scope
├── child fiber
│   └── its child scope
├── resource finalizer
└── resource finalizer
```

Closing a scope means:

```text
1. prevent new registrations
2. signal children to stop
3. wait for all children
4. run finalizers in reverse acquisition order
5. return cleanup Cause, if any
```

The order matters: children must terminate before resources on which they may depend are released.

ZIO similarly treats `Scope` as the foundation of composable resource lifetime management, with scope closure running registered finalizers.

## 3.1 Public `Scope` should be a handle, not an exposed mutable object

For example:

```go
type Scope struct {
    state *scopeState
}
```

The internal state can contain:

```go
type scopeState struct {
    mu sync.Mutex

    state scopeStatus

    ctx    context.Context
    cancel context.CancelCauseFunc

    children sync.WaitGroup
    finalizers []finalizer
}
```

`Scope` itself remains cheap and copyable; the synchronization primitives remain behind a pointer and therefore are never copied.

## 3.2 Scope closure must be concurrency-safe

There is an important race:

```text
goroutine A                    goroutine B

resource acquired
                              scope starts closing
register finalizer
```

If registration and closure are not coordinated, a successfully acquired resource can leak.

Registration therefore needs an atomic relationship with the scope state:

```text
open:
    register finalizer

closing/closed:
    registration rejected
    newly acquired resource immediately finalized
```

The mutex here is preferable to contorting lifecycle bookkeeping into channels. Go itself recommends channels for higher-level coordination, but low-level synchronization state is an appropriate use of `sync` primitives.

---

# 4. Make lexical scopes explicit without putting `Scope` into `R`

ZIO can express:

```text
ZIO[R & Scope, E, A]
```

and remove `Scope` when `scoped` is applied.

Our structural `Product` encoding cannot conveniently normalize or subtract a `Scope` buried inside arbitrary nested `Product[R1,R2]`.

Forcing scope into `R` would therefore substantially damage usability.

Instead use an explicit lexical constructor:

```go
Scoped(func(scope Scope) Effect[R, E, A] {
    ...
})
```

This has several advantages:

- resource ownership is visible;
- no `context.Value` magic;
- `R` remains about application requirements;
- the scope cannot exist before execution;
- scope creation and destruction remain controlled by the runtime.

Go 1.27 makes generic methods on `Scope` practical, so resource use can remain fluent:

```go
Scoped(func(scope Scope) Effect[Env, AppError, Result] {
    return scope.
        AcquireRelease(openDatabase(), closeDatabase).
        FlatMap(runQuery)
})
```

`Scope` should initially expose no public `Close`. The code that creates a scope owns closure, preventing another component from prematurely destroying someone else's lifetime boundary. This is also the ownership distinction ZIO makes between ordinary scopes and closeable scopes.

---

# 5. Fiber representation

A first instinct would be:

```go
type Fiber[E, A any] struct {
    result <-chan Exit[E, A]
}
```

That is wrong.

Receiving the `Exit` consumes it, making the fiber effectively single-observer.

`Await` and `Join` need to be repeatable by arbitrarily many waiters.

## Correct Go representation

Use:

```go
type Fiber[E, A any] struct {
    state *fiberState[E, A]
}

type fiberState[E, A any] struct {
    id FiberID

    done   chan struct{}
    cancel context.CancelCauseFunc

    result Exit[E, A]
}
```

The child:

```text
computes Exit
    ↓
writes result
    ↓
close(done)
```

All waiters:

```text
<-done
   ↓
read immutable result
```

This is particularly idiomatic Go because closing a channel is a broadcast operation.

It is also memory-safe without putting a mutex around `result`: Go's memory model guarantees that closing a channel synchronizes before a receive which observes that closure. Writes performed before `close(done)` are therefore visible to the waiter afterward.

This supports unlimited:

```go
fiber.Await()
fiber.Join()
fiber.Poll()
```

calls.

---

# 6. Fiber API

The initial public API should remain small.

```go
type Fiber[E, A any]
```

with approximately:

```go
func (f Fiber[E, A]) Await() Effect[Unit, Never, Exit[E, A]]

func (f Fiber[E, A]) Join() Effect[Unit, E, A]

func (f Fiber[E, A]) Interrupt() Effect[
    Unit,
    Never,
    Exit[E, A],
]

func (f Fiber[E, A]) Done() <-chan struct{}

func (f Fiber[E, A]) Poll() (Exit[E, A], bool)
```

`Done()` is intentionally non-effectful interoperability with ordinary Go:

```go
select {
case <-fiber.Done():
case <-ctx.Done():
case msg := <-events:
}
```

It exposes synchronization, **not a consumable result channel**.

## Await

`Await` waits for termination and succeeds with the complete result:

```text
Fiber[E,A]
    ↓ Await
Effect[Unit, Never, Exit[E,A]]
```

Waiting itself remains interruptible.

## Join

`Join` resurfaces the child's channels:

```text
Fiber[E,A]
    ↓ Join
Effect[Unit, E, A]
```

- typed child failure becomes typed caller failure;
- defect remains defect;
- interruption becomes interruption.

This matches the useful distinction between `await` and `join` in ZIO.

## Interrupt

`Interrupt` should mean:

```text
request cancellation
        ↓
wait for fiber
        ↓
wait for its child scopes
        ↓
wait for its resource finalizers
        ↓
return final Exit
```

It must not merely invoke a cancellation function and return.

ZIO deliberately specifies interruption this way to avoid resource leaks.

If later we need fire-and-forget cancellation, give it a distinct, explicitly weaker name rather than weakening `Interrupt`.

---

# 7. Fork semantics and structured concurrency

## Default `Fork`

```go
func Fork[R, E, A any](fx Effect[R, E, A]) Effect[
    R,
    Never,
    Fiber[E, A],
]
```

`Fork` should:

1. capture `R`;
2. create a child context with `context.WithCancelCause`;
3. register the child in the current scope;
4. start one goroutine;
5. immediately return its `Fiber`.

Inside the child, create a new child scope for its own resources and descendants.

This gives a tree:

```text
root scope
│
├── fiber A
│   └── scope A
│       ├── fiber A1
│       └── resources
│
└── fiber B
    └── scope B
```

No child created through the effect API can silently become an orphan.

## Scoped fork

The safest Go-oriented rule is:

> Ordinary `Fork` attaches to the **current dynamic scope**.

Therefore a fork inside:

```go
Scoped(func(scope Scope) ...)
```

cannot outlive resources acquired in that scope.

For advanced cases where a child intentionally needs another lifetime:

```go
scope.Fork(effect)
```

or:

```go
effect.ForkIn(scope)
```

can make that ownership explicit.

This is slightly stronger and less footgun-prone than blindly reproducing ZIO's precise parent-fiber versus `forkScoped` distinction.

## Detached/daemon fibers

Do not introduce `ForkDaemon` until there is an explicit long-lived `Runtime`.

A detached fiber needs an owner.

The eventual model should be:

```go
runtime := effect.NewRuntime(...)
defer runtime.Close(...)

runtime.Run(...)
```

and:

```text
ForkDaemon -> runtime root scope
```

Thus even a "daemon" fiber is not genuinely unowned; it is bounded by `Runtime.Close`.

The package-level:

```go
Run(...)
```

can use an ephemeral runtime whose root is closed before `Run` returns.

---

# 8. Use Go 1.27 `WaitGroup.Go` inside scopes

Go 1.27 requires no compatibility concession here.

`sync.WaitGroup.Go` was introduced in Go 1.25 and is now the preferred API over manually pairing `Add`/`Done`. It also supports tasks starting further tracked tasks while the group is non-empty.

A scope can therefore use:

```go
scope.children.Go(func() {
    runFiber(...)
})
```

under its registration gate.

The documented requirement that the passed function must not panic is compatible with the runtime because `Effect.run` already converts user panics into defects before they escape the fiber runner. Internal runtime panics remain library bugs.

`errgroup` should **not** form the core abstraction. It is excellent Go infrastructure, but it propagates a single ordinary `error`, whereas we need typed `E`, complete `Cause` trees, repeatable fiber observation, scopes and explicit interruption semantics. Its API is nevertheless a useful reference for cancellation and bounded concurrency.

---

# 9. Resource management

## 9.1 Scope finalizers are LIFO

Resources frequently depend on resources acquired before them:

```text
database
    ↓
transaction
    ↓
statement
```

so destruction should reverse acquisition:

```text
statement
transaction
database
```

Use a finalizer stack.

## 9.2 Finalizers must not have arbitrary typed errors

A scope may contain unrelated resources:

```text
DB release error       = DBReleaseError
file release error     = FileCloseError
server release error   = ServerShutdownError
```

A heterogeneous scope cannot add those types to `E` after the resource effects have already been composed.

The principled rule should therefore match ZIO's scoped-resource design:

> Scope finalizers have a `Never` typed-failure channel.

They may still **defect**, and those defects are preserved in `Cause`.

ZIO likewise makes release workflows of ordinary scoped resources infallible at the typed-error level.

If a Go `Close() error` represents a meaningful domain failure, the caller must explicitly decide how to handle it before registration:

- log/absorb it;
- convert it to a defect;
- use a lexical acquire/use/release combinator whose release error can participate in `E`.

Do not silently discard it.

## 9.3 Acquisition should remain Go-context-aware

Do **not** mechanically copy ZIO's "uninterruptible acquisition" by stripping cancellation from every acquisition context.

In Go that would actively fight idiomatic APIs such as:

```go
db.QueryContext(...)
http.NewRequestWithContext(...)
net.Dialer.DialContext(...)
```

and could turn cancellation into indefinite waits.

Instead guarantee:

> If acquisition returns a successfully acquired resource, its finalizer is registered atomically before the runtime performs another interruption boundary.

If acquisition is interrupted before returning a resource, the acquisition implementation owns cleanup of any partially created state, which is the conventional Go API contract.

This is a deliberate and important Go-specific divergence.

## 9.4 Cleanup should survive caller cancellation

When the parent context has already been canceled, using it for release would frequently cause release to abort immediately.

Finalization should therefore run with a cleanup context detached from normal cancellation, approximately via:

```go
context.WithoutCancel(ctx)
```

which preserves contextual values while removing cancellation. Go documents that `WithoutCancel` also removes deadlines and cancellation causes.

Because an uncooperative finalizer can then hang indefinitely, the future `Runtime` should support an optional cleanup timeout policy. The default core semantics should favor correctness/resource release rather than silently truncating cleanup.

## 9.5 Finalizer defects compose sequentially

Suppose:

```text
main effect     -> Fail(E)
finalizer #2    -> Die(D2)
finalizer #1    -> Die(D1)
```

the final result should preserve:

```text
Then(
    Fail(E),
    Then(
        Die(D2),
        Die(D1),
    ),
)
```

rather than throwing away all but one failure.

---

# 10. Effect-level cleanup operators

Once `Cause` and `Scope` exist, add:

```go
Ensuring(...)
OnExit(...)
```

with the same cleanup guarantees.

Then implement:

```go
scope.AcquireRelease(...)
```

on top of scope registration rather than creating an unrelated resource mechanism.

One lifetime mechanism should power:

- fibers;
- resources;
- layer resources;
- explicit finalizers;
- future streams.

This avoids multiple subtly different cleanup systems.

---

# 11. Parallel composition

After Fiber and Scope are stable, add high-level concurrency operators.

## `ZipPar`

```go
ZipPar(fx, that)
```

should:

```text
create private scope
        ↓
fork left + right
        ↓
wait concurrently
        ↓
success + success -> Product
failure           -> interrupt unfinished sibling
        ↓
wait for sibling cleanup
        ↓
close private scope
```

For heterogeneous channels:

```text
R = Product[R1,R2]
E = Either[E1,E2]
A = Product[A1,A2]
```

remains exact.

If both sides independently fail, preserve both using:

```text
Cause.Both(...)
```

If the second side only becomes interrupted because the first side failed, do not misleadingly report that induced interruption as an independent failure.

Internal cancellation causes should therefore distinguish cases such as:

```text
user interruption
parent scope closed
parallel sibling failed
race lost
timeout
```

using `context.WithCancelCause`.

ZIO's parallel operators similarly cancel unnecessary sibling work when one branch fails.

---

# 12. Racing

Provide two deliberately distinct semantics.

## `Race`

Follow ZIO-style semantics:

```text
return first success
```

If one branch fails, keep waiting for the other.

If both fail:

```text
Cause.Both(left, right)
```

When one succeeds, interrupt and await the loser before returning.

ZIO's `race` is explicitly first-success rather than first-completion.

## `RaceFirst`

Also provide the Go-natural alternative:

```text
first completion wins
```

This maps directly onto:

```go
select {
case <-left.Done():
case <-right.Done():
}
```

Having both avoids an ambiguous `Race` API.

---

# 13. Collection concurrency

After binary operations are correct:

```go
AllPar(...)
ForEachPar(...)
ForEachParN(...)
```

should follow.

For bounded parallelism, prefer an internal buffered channel semaphore initially:

```go
permits := make(chan struct{}, n)
```

rather than adding a core dependency.

`x/sync/errgroup` supports `SetLimit` and `TryGo` and is useful prior art, but its `error`-centric semantics do not justify making it a core dependency.

Preserve result ordering for collection combinators even though execution order is concurrent.

---

# 14. Native channel integration

Channels should be treated as a strength of Go, not hidden behind an incompatible concurrency universe.

## Send

Provide something like:

```go
Send(ch, value)
```

whose implementation is essentially:

```go
select {
case ch <- value:
    return success
case <-ctx.Done():
    return interrupted
}
```

Its typed failure channel should be `Never`.

### Sending to a closed channel

Do **not** invent:

```text
ChannelClosedError
```

for send.

Go provides no race-free general test that guarantees another goroutine cannot close a channel between the test and the send.

Sending on a closed channel is defined to panic.

In properly structured Go, channel closure is an ownership/protocol issue: the sending side responsible for closure must not send afterward.

Therefore a send to a closed channel is a **defect**, exactly as the Go panic indicates.

## Receive

Receiving from a closed channel is not exceptional in Go. It produces:

```go
value, ok := <-ch
```

with `ok == false`.

Preserve that semantic rather than turning closure into `E`.

For example:

```go
type Receive[A any] struct {
    Value A
    OK    bool
}
```

and:

```go
Recv(ch) Effect[Unit, Never, Receive[A]]
```

A more opinionated `RecvOrFail` can be layered on top when applications genuinely regard closure as a typed error.

## Nil channels

A raw receive/send on a nil channel blocks forever, but putting it in a `select` alongside `ctx.Done()` makes the effect interruptible. This follows Go's native `select` semantics.

## Channel ownership

Do not create a generic abstraction that automatically closes arbitrary channels.

Continue the conventional Go rule:

> The producer/owner decides when a channel is closed.

The official Go pipeline guidance follows the same pattern: stages close their outbound channels and cancellation is separately signalled so blocked producers can terminate.

---

# 15. Do not wrap every channel in an effect-specific channel type

Initially support:

```text
native chan A
    +
effectful Send/Recv
```

rather than immediately introducing:

```go
Channel[E, A]
```

Go channels already provide:

- buffering;
- backpressure;
- synchronization;
- MPMC operation;
- directional typing;
- `select`;
- ecosystem interoperability.

An effect-specific queue/deferred abstraction should only be introduced where it adds semantics that native channels genuinely lack.

For example, a future:

```go
Deferred[E, A]
```

would sensibly reuse the same mechanism as Fiber completion:

```text
stored Exit + closed done channel
```

because it needs repeatable broadcast observation, unlike a consumable channel message.

---

# 16. Runtime representation

Structured concurrency requires a little more runtime context than today's:

```go
eval func(context.Context, R) Exit[E, A]
```

Do not misuse `context.Value` to smuggle runtime internals through Go contexts.

Instead evolve the private evaluator boundary to something conceptually like:

```go
eval func(
    context.Context,
    *runtimeState,
    R,
) Exit[E, A]
```

where `runtimeState` provides internal information such as:

```text
current scope
runtime root
fiber identity
runtime configuration
future supervisor/tracing hooks
```

`context.Context` remains explicit and first, consistent with normal Go APIs. The Go context documentation specifically recommends explicit context propagation rather than using contexts as general optional-parameter containers.

This remains entirely private. The public `Effect[R,E,A]` API need not expose runtime machinery.

---

# 17. Base runtime capabilities

The base package should include the small effectful capabilities required by most
programs: initially `Clock`, `FileSystem` and `Logger`. Requiring every application
to invent wrappers for these would fragment cancellation, testing, errors and
observability before the ecosystem has common conventions.

These are **runtime capabilities**, analogous to the default services available
in ZIO and Effect-TS. Their public effect constructors retain ordinary
`Effect[R,E,A]` typing, but the capabilities themselves are supplied by
`runtimeState`, not hidden in `context.Value` and not added to application `R`.
This keeps basic effects usable as:

```text
Now                         Effect[Unit, Never, time.Time]
ReadFile(path)              Effect[Unit, IOError, []byte]
LogInfo(message, fields...) Effect[Unit, Never, Unit]
```

An application may still put a domain-specific storage, audit or logging service
in `R`. The base capabilities are low-level process facilities, not a service
locator for arbitrary dependencies.

## 17.1 Live defaults and runtime-local overrides

Every runtime receives a complete immutable capability set:

```go
type RuntimeCapabilities struct {
    Clock      Clock
    FileSystem FileSystem
    Logger     Logger
}
```

Names and exact interfaces can change during implementation, but the invariants
are:

- package-level `Run` uses safe live defaults;
- `NewRuntime` accepts explicit replacements for tests and embedding;
- an override is local to that runtime and never mutates package-global state;
- nil capabilities are rejected or filled with defaults at construction, not
  discovered as a panic during effect execution;
- a running interpretation sees a stable capability set.

Avoid global setters such as `SetDefaultLogger`. They make parallel tests race and
make library behavior depend on initialization order.

## 17.2 Clock

The clock capability powers `Now`, `Sleep`, schedules, timeout and retry. The
requirements in section 20 still apply: waits are interruptible, live time retains
monotonic behavior, and a deterministic test clock can advance without wall-clock
sleeping.

The public clock contract should expose only the operations the runtime can
implement correctly. In particular, do not expose a raw `time.Timer` unless the
test clock can reproduce its stop/reset/channel semantics. A private timer
interface or an interruptible `Sleep(context.Context, duration)` contract is
preferable to leaking the entire standard-library timer API.

## 17.3 FileSystem

Provide a deliberately small filesystem surface first:

```go
ReadFile(path)
WriteFile(path, data, permissions)
Stat(path)
ReadDir(path)
MkdirAll(path, permissions)
Remove(path)
Rename(oldPath, newPath)
```

Open file handles are resources and belong in the Scope phase; do not return a
bare handle without a release story.

Filesystem failures should use a stable typed wrapper:

```go
type IOError struct {
    Op   string
    Path string
    Err  error
}
```

It should implement `error` and `Unwrap`, preserve `errors.Is`/`errors.As`, and
avoid flattening platform-specific error information into strings. Operations
with two paths should record both in an unambiguous form.

Most `os` package calls do not accept a context and cannot be forcibly canceled.
Check cancellation before and after them, but do not move each call to an
unowned goroutine merely to fake prompt cancellation. Context-aware filesystem
implementations may cooperate more strongly through the capability interface.

The live implementation delegates to `os`/`io/fs`. Tests should normally use a
temporary directory; a small in-memory implementation is useful only if its
documented semantics do not pretend to reproduce every OS permission, symlink,
locking and atomic-renaming behavior.

## 17.4 Logger

Logging is an effect so construction remains lazy and records inherit runtime
metadata:

```go
Log(level, message, fields...)
LogDebug(...)
LogInfo(...)
LogWarn(...)
LogError(...)
```

Use a typed `LogRecord` containing at least timestamp, level, message, structured
fields, fiber identity, span context and annotations. Field order should remain
stable for deterministic tests and renderers. Reserve standard keys and define
duplicate-key behavior rather than relying on unordered `map[string]any` output.

The logging effect has a `Never` typed-failure channel. A logger implementation
must therefore either complete or report an implementation failure to the
runtime diagnostics sink; it must not silently manufacture an application `E`.
Provide an explicit best-effort adapter for sinks where dropping logs is intended.

Logger calls run inline by default. An asynchronous logger owns its buffer and
worker lifetime through the explicit `Runtime`; it must specify overflow,
flush-on-close and shutdown-timeout policies. The core runtime must not spawn an
unowned logging goroutine.

Sensitive fields require explicit redaction support. Rendering a cause, request
or environment must never reflect arbitrary values automatically.

---

# 18. Observability and developer experience

Observability affects runtime data structures, so postponing it would force
invasive redesign. Exporter integrations can wait; stable internal hooks and
metadata cannot.

The goal is not to trace every `Map` call. The goal is to make fibers, scopes,
retries, resources and user-named operations understandable while preserving
normal effect semantics.

## 18.1 Runtime events and observers

Define a small immutable event model for meaningful lifecycle boundaries:

```text
fiber started/completed/interrupted
scope opened/closing/closed
resource acquired/released
span started/ended
retry scheduled/exhausted
log record emitted
runtime closing/closed
```

Events should carry stable IDs, monotonic timestamps/durations, parent IDs and a
compact final status. Do not attach arbitrary environments or successful values.

Observers/supervisors are installed per runtime. The no-op observer must be cheap,
and event construction for disabled categories should be avoided. An observer
must not change an effect's `Exit`: panics and internal observer failures are
contained and routed to a diagnostics fallback rather than becoming application
defects. Blocking/exporting observers need an explicitly owned queue with a
documented backpressure/drop policy.

## 18.2 Spans, names and annotations

Provide effect-level operations along these lines:

```go
fx.Named(name)
fx.WithSpan(name, attributes...)
fx.Annotate(key, value)
```

Names and annotations flow through the private runtime state and are inherited by
child fibers. They are not smuggled through `context.Value`. When adapting to an
ecosystem tracer such as OpenTelemetry, the adapter may derive the conventional
context value at the external API boundary, while the effect runtime retains its
own explicit representation.

Span final status must distinguish success, typed failure, defect and
interruption. Parallel causes and finalizer defects must remain visible rather
than being collapsed to one `error` string.

Source locations are valuable but `runtime.Caller` on every combinator is too
expensive and noisy. Capture them only for named/span boundaries or under an
explicit debug option.

## 18.3 Cause rendering and diagnostics

The compositional cause algebra needs a deterministic tree renderer that shows:

```text
Fail / Die / Interrupt
Then versus Both structure
fiber and span provenance when available
panic stacks
suppressed/cleanup failures
```

Provide both a human-readable formatter and a structured representation suitable
for logs/exporters. Formatting must be total: zero/empty causes, nil wrapped
errors, non-string panic values and malformed external metadata must not make the
renderer panic.

`Exit` and `Cause` should implement useful `fmt.Formatter`/`String` behavior, but
keep machine-readable inspection methods so tests and tools never need to parse
display text.

## 18.4 Metrics and cardinality

The runtime should expose counters/histograms for lifecycle and scheduling data,
including active fibers, fiber duration, scope close duration, retries, retry
delay, defects and interruptions. The core defines events/measurements, not a
mandatory metrics backend.

Never use messages, paths, error strings or arbitrary annotation values as metric
labels by default. Export adapters must enforce a bounded label vocabulary to
avoid cardinality and sensitive-data incidents.

## 18.5 Test and debugging experience

The base package should make deterministic tests straightforward:

- manually advanced clock;
- recording logger and observer;
- runtime-local filesystem replacement or temporary-directory helper;
- fiber/scope leak assertions when a test runtime closes;
- stable cause/exit renderers suitable for golden tests;
- explicit runtime configuration snapshots in diagnostic output.

Add a debug runtime mode that records ownership edges and reports still-live
fibers/resources at close. It may cost more and should not be the production
default.

All public effects and combinators need concise Go doc examples, and error
messages should state the violated invariant and relevant IDs. Avoid APIs whose
only documentation is analogy to ZIO or Effect-TS; Go users should be able to
understand behavior from this project alone.

## 18.6 Integration boundary

OpenTelemetry, slog adapters and vendor exporters belong in optional integration
packages. The base package owns:

```text
LogRecord and Logger
span/event data model
observer hook
cause rendering
runtime capability/test interfaces
```

This keeps the runtime observable without adding a required dependency or tying
its semantics to one telemetry ecosystem.

---

# 19. Layer integration

Once scoped resources work, `Layer` needs to adopt exactly the same lifetime mechanism.

Do not create a special resource system for layers.

A scoped layer should effectively mean:

```text
build resource(s)
register finalizers in owning scope
produce ROut
consumer runs
scope closes
```

Provide a constructor along the lines of:

```go
LayerScoped(func(scope Scope) Effect[RIn, E, ROut])
```

Resourceful services produced by a layer should therefore live exactly as long as the effect to which the layer is provided.

This avoids a class of bugs where the layer's construction scope ends before the provided service has finished being used—an issue explicitly called out in ZIO's layer/resource documentation.

---

# 20. Retry and scheduling

Retry is part of the effect semantics, not merely a helper loop. It must preserve
typed failures, cancellation, resource lifetimes, laziness and deterministic
testability.

The runtime must still obey the architectural boundary from section 1:

> A schedule decides **whether and when to evaluate again**. Go's runtime still
> schedules every goroutine.

There should be no timer wheel, worker thread, global retry goroutine or background
"effect scheduler" in the initial runtime.

## 20.1 Model schedules as immutable descriptions with per-run drivers

Use a typed schedule abstraction conceptually like:

```go
type Schedule[In, Out any] struct {
    // opaque, immutable description
}

type Decision[Out any] struct {
    Continue bool
    Delay    time.Duration
    Output   Out
}
```

The exact private representation may use closures around an internal state type.
The important separation is:

```text
Schedule value          immutable and reusable
    ↓ start
Schedule driver         fresh mutable state for one interpretation
    ↓ input + clock time
Decision                continue/done, delay and observable output
```

A single `Schedule` value must therefore be safe to reuse concurrently. State such
as retry count, elapsed time or the previous Fibonacci delay belongs to a newly
created driver, never to the shared schedule value.

`In` makes the same machinery useful for both:

```text
Retry   input = typed failure E
Repeat  input = successful value A
```

`Out` allows policies to expose useful state such as attempt count, elapsed time
or the last computed delay without weakening the effect's `E` channel.

## 20.2 Provide a runtime clock boundary

All time-aware built-ins must use one injectable clock abstraction owned by the
runtime state:

```text
live Runtime    -> time.Timer / time.Now
test Runtime    -> manually advanced deterministic clock
```

Do not call `time.Sleep` directly. A live wait should use a timer and `select` on
both the timer and `ctx.Done()`, stopping/draining the timer as necessary when
cancellation wins.

The clock is a built-in runtime service rather than an application requirement in
`R`. Otherwise every use of `Sleep`, `Timeout` or `Retry` would change the public
environment type and exacerbate nested `Product` composition. `Runtime` should
allow an explicit clock override for tests; the package-level `Run` uses the live
clock.

Durations and elapsed-time limits need precise rules:

- the first attempt/run is immediate;
- retry delays are measured after the failed attempt completes;
- negative delays are normalized to zero;
- duration arithmetic saturates instead of overflowing;
- elapsed time is measured with the injected clock and, for the live clock, uses
  Go's monotonic time component;
- a canceled clock wait produces `Interrupt`, preserving `context.Cause(ctx)`.

## 20.3 Retry only typed failures by default

The primary operator should have semantics equivalent to:

```go
func (fx Effect[R, E, A]) Retry[Out any](
    policy Schedule[E, Out],
) Effect[R, E, A]

func (fx Effect[R, E, A]) RetryCause[Out any](
    policy Schedule[Cause[E], Out],
) Effect[R, E, A]
```

Interpretation is:

```text
evaluate fx immediately
    ├── success          -> return A
    ├── Die              -> return Die; do not retry
    ├── Interrupt        -> return Interrupt; do not retry
    └── Fail(E)
          ↓ feed E to fresh schedule driver
        done             -> return the last Fail(E)
        continue(delay)  -> wait interruptibly, then evaluate fx again
```

Before asking the policy or beginning another attempt, check interruption. If the
attempt reports a typed failure after its context has already been canceled, the
observed interruption wins and retrying stops.

This default is important: retrying defects can repeat corrupting program logic,
and retrying interruption would violate structured cancellation. A future
operator for retrying defects may be added under an explicitly dangerous name,
but `Retry` must never retry `Die` or `Interrupt`.

The compositional `Cause` algebra makes eligibility slightly more subtle than a
three-way switch. The ordinary `Retry(Schedule[E, Out])` should retry only an
exact `Fail(E)` leaf. It must not silently choose one failure from
`Both(Fail(e1), Fail(e2))`, and it must not discard a cleanup defect from
`Then(Fail(e), Die(d))`. Provide a separate, explicit
`RetryCause(Schedule[Cause[E], Out])` for callers that want to retry an
all-typed-failure composite cause. Even that operator must reject any cause tree
containing `Die` or `Interrupt`; a genuinely unsafe defect-retry API would be a
different future operation.

When the policy is exhausted, `Retry` returns the **last typed failure**, not a
generic "retries exhausted" error. This keeps its type stable. Provide a richer
combinator for callers that need policy output or fallback behavior, for example:

```go
RetryOrElse(policy, func(last E, out Out) Effect[R, E2, B])
```

with normal `Product`/`Either` structural variants when the fallback changes
requirements or error types.

Conveniences should have unambiguous counting. In particular:

```go
fx.RetryN(3) // at most 3 retries after the initial attempt; 4 attempts total
```

Do not use an ambiguous `Attempts(3)` name unless it explicitly includes the
initial execution in both its name and documentation.

## 20.4 Repetition and delayed execution use the same machinery

Scheduling is more general than retry. Add these operations from the same clock
and schedule substrate:

```go
Sleep(duration)                 // interruptible, Never typed failure
fx.Delay(duration)              // delay before first evaluation
fx.Repeat(Schedule[A, Out])     // schedule from successful outputs
fx.Timeout(duration)            // race against the runtime clock
```

`Repeat` evaluates once immediately, feeds each successful `A` to its fresh
driver, and runs again only when the decision says to continue. Any typed failure,
defect or interruption stops repetition immediately. Its primary result should be
the schedule's final `Out`; a convenience that preserves the last `A` can be
provided separately.

Repeated execution is sequential by default: one run finishes before the policy
delay and the next run. It never overlaps executions silently. Users who want
overlap must make concurrency explicit with `Fork` and give it a scope.

Fixed-delay and fixed-rate timing are distinct and must not share an ambiguous
constructor:

- `Spaced(d)` waits `d` after completion before starting again;
- a later `FixedRate(d)` uses planned start times, does not overlap runs, and skips
  missed ticks rather than starting a catch-up burst.

Calendar/cron scheduling, persistence across process restarts and distributed job
leasing are outside the core runtime. They need wall-clock/calendar and ownership
semantics that are substantially different from retry delays.

## 20.5 Initial policy constructors and composition

Start with a small policy vocabulary:

```text
Stop
Forever
Recurs(n)              n continuations after the first execution
Spaced(d)
Exponential(base, cap)
Fibonacci(one, cap)
UpTo(limit)
WhileInput(predicate)
WhileOutput(predicate)
MapOutput(f)
Jittered(random, min, max)
```

Jitter is important operationally to prevent synchronized clients from retrying
in lockstep, but randomness must be injected so tests can be deterministic. Do not
use package-global random state implicitly.

Policy combination can follow these explicit rules:

```text
AndSchedules(left, right) continues only while both continue; waits max(left, right)
OrSchedules(left, right)  continues while either continues; waits min(active delays)
```

The output types remain exact (`Product` for `And`; a named combined decision or
`Either`-based form for `Or`). These operations should be added only with law and
boundary tests; a pile of subtly inconsistent schedule combinators is worse than
a small predictable algebra.

ZIO schedules can require an environment and Effect-TS schedules can themselves
fail and require services. That expressiveness is useful for advanced policies,
but it recursively turns policy evaluation into another effect problem. The first
Go schedule algebra should be pure and infallible apart from a defect caused by a
panicking user callback. Add effectful policies later only when concrete use cases
cannot be expressed by retry hooks or by building a policy before `Retry`.

## 20.6 Resource and scope semantics across attempts

`Retry` re-evaluates the effect in the current dynamic scope. It must not invent a
hidden scope and close it after a successful attempt: the returned `A` may refer
to a resource intentionally owned by the caller's scope.

This means placement of `Scoped` is significant:

```text
Scoped(useResource).Retry(policy)
    each attempt owns and closes its resources before Retry observes its Exit

Scoped(func(scope) { useResource.Retry(policy) })
    all attempts share the outer scope; registered resources live until it closes
```

The first form should be the documented default when retrying an acquire/use
workflow. The second is valid for deliberately shared resources, but failed
attempts can retain registrations until the outer scope closes.

Never rerun an arbitrary finalizer as part of retry. Finalizers run according to
their owning scope, exactly once per successful registration.

## 20.7 Timeout and cancellation must remain structured

`Timeout` is logically a race between the effect and a clock wait, but it must use
the same private-scope algorithm as `RaceFirst`:

```text
timeout wins
    ↓ cancel effect with a distinct timeout cause
    ↓ await child and all finalizers
    ↓ return timeout result
```

It must not return while the timed-out work is still executing. Offer separate
forms for common error-channel choices, such as an optional result and a caller
supplied typed timeout error, rather than smuggling a generic timeout error into
every `E`.

Retry hooks (logging, metrics and tracing) must observe attempt number, last typed
failure, selected delay and elapsed time. Hooks should be ordinary effects with a
`Never` typed-failure channel so they inherit cancellation and defect semantics;
they must not require the core schedule driver to know about logging.

## 20.8 Required retry/schedule tests

At minimum cover:

- first attempt is immediate;
- exact `RetryN` attempt counts, including zero;
- last typed failure is preserved on exhaustion;
- success stops retrying;
- defects and interruption are never retried;
- composite typed failures require `RetryCause`, while any composite containing a
  defect or interruption is never retried;
- cancellation during an attempt and during a delay;
- cancellation cause preservation;
- fresh independent driver state for concurrent reuse of one policy;
- deterministic virtual-clock advancement without wall-clock sleeps;
- exponential/Fibonacci overflow and caps;
- elapsed-time boundaries and zero/negative delays;
- deterministic jitter bounds;
- `And`/`Or` continuation and delay laws;
- resource/finalizer behavior for both scope placements described above;
- timeout awaits losing-child cleanup;
- repetition stops on typed failure, defect and interruption;
- no timer or goroutine leaks under `go test -race`.

---

# 21. Sequencing ergonomics and the limits of syntactic sugar in Go

Long `FlatMap` chains are a real usability problem, especially when later steps
depend on several earlier values. However, ZIO's `for` comprehension and
Effect-TS's `yield*` rely on language-level desugaring or resumable generators.
A Go library cannot add either facility.

Go's range-over-function iterator syntax is not an equivalent generator
mechanism. Its `yield` callback sends zero, one or two values from the iterator to
the loop; the loop cannot resume the producer with an arbitrarily typed result.
It also requires one homogeneous iteration type. Encoding heterogeneous effect
results through `any`, reflection and hidden goroutines would lose the static
channels and complicate cancellation, so it should not be used as fake do
notation.

## 21.1 Prefer combinators that remove unnecessary dependencies

Before introducing new syntax, provide and document the operators that avoid
`FlatMap` in the first place:

- `Map`, `Zip` and `ZipPar` for independent work;
- `All`/`ForEach` and their parallel variants for homogeneous collections;
- `As`, `Tap`, `Flatten` and `AndThen` for common sequencing shapes;
- small named functions for meaningful workflow stages.

Go does not infer generic function arguments from an expected result type. A
zero-argument primitive such as `Now[R, E]()` therefore cannot infer either
phantom channel even when its result is immediately passed to `Zip`. Do not make
users repeat those arguments throughout a program. Provide a stateless typed
constructor carrier:

```go
operations := For[Env, AppError]()
now := operations.Now()
log := operations.LogInfo("starting")

// Common base-capability programs can select their standard channels without
// any type arguments at all.
io := IO() // IOOperations[Unit], error channel IOError
loaded := Zip(io.ReadFile(inputPath), io.Now())
```

`Operations[R, E]` contains no runtime services; it only carries compile-time
channel evidence into ordinary constructors. `IOOperations[R]` adds filesystem
operations whose typed error is `IOError`. The original generic functions remain
available as low-level primitives. This centralizes unavoidable annotation
without introducing reflection, type erasure or a service locator.

Dense application code may use an ordinary import alias such as `fx`. Do not
recommend dot imports as syntax sugar: losing symbol provenance and increasing
name collisions is a poor trade for removing a short qualifier. Foundational
documentation should retain the explicit `effect` package name; focused examples
may use `fx` when it materially improves readability.

Generic methods in Go 1.27 make these fluent and allow the output type to change,
but they do not change the fundamental need for a callback when a later effect
depends on an earlier result.

## 21.2 A safe state-builder can flatten layout

The first optional ergonomic layer should be a typed state builder. It does not
pretend to be new syntax; it packages successive `FlatMap` operations while
keeping source code visually flat.

This is the closest Go analogue to Effect-TS's non-generator `Do`/`bind` record
API. TypeScript can grow a structural record type after every bind; Go cannot, so
the Go version uses one caller-declared state type for the whole workflow.

Conceptually, use value-returning state updates so the builder encourages local,
expression-oriented state rather than shared mutation:

```go
type checkoutState struct {
    user   User
    cart   Cart
    quote  Quote
}

operations := For[Env, AppError]()
program := operations.Do(func() checkoutState { return checkoutState{} }).
    Bind(
        func(s checkoutState) Effect[Env, AppError, User] {
            return LoadUser()
        },
        func(s checkoutState, user User) checkoutState {
            s.user = user
            return s
        },
    ).
    Bind(
        func(s checkoutState) Effect[Env, AppError, Cart] {
            return LoadCart(s.user.ID)
        },
        func(s checkoutState, cart Cart) checkoutState {
            s.cart = cart
            return s
        },
    ).
    Bind(
        func(s checkoutState) Effect[Env, AppError, Quote] {
            return Price(s.user, s.cart)
        },
        func(s checkoutState, quote Quote) checkoutState {
            s.quote = quote
            return s
        },
    ).
    Yield(func(s checkoutState) Result {
        return Result{User: s.user, Quote: s.quote}
    })
```

The state factory must run once **per effect interpretation**, not when the effect
is constructed. This preserves laziness and prevents state from being shared
across retries or concurrent executions. `Bind` is just typed `FlatMap` plus a
state transition, so failures, defects, cancellation and stack-safety behavior
stay identical to the core operators.

The builder is sequential. A state value can still contain pointers, slices or
maps, so callbacks must not publish and mutate referenced data concurrently;
workflows that intentionally share state should use a proper synchronized service.

The initial builder should support one fixed `R` and `E`. Callers adapt individual
steps with `Provide`, `MapError` or named domain contracts. A `BindMerge` can be
added later if nested `Product`/`Either` types remain readable; do not erase them
with `any`.

The builder trades indentation for an explicit state struct and transitions. It is
most useful for workflows with several cross-step dependencies, not for every
two-step composition. It should live as a thin optional API implemented entirely
in terms of the core algebra.

## 21.3 Direct-style `Bind` is possible only with exceptional control flow

An API shaped like this is superficially attractive:

```go
program := Direct(func(d *DirectContext[Env, AppError]) Result {
    user := d.Bind(LoadUser())
    cart := d.Bind(LoadCart(user.ID))
    return Checkout(user, cart)
})
```

But `Bind` must return an `A` while also immediately abandoning the callback on
failure. Ordinary Go functions have no resumable suspension or typed early-return
protocol that can do this. A library implementation must use a private panic
sentinel (or an even less suitable goroutine/channel coroutine) to jump back to
the `Direct` interpreter.

A carefully contained sentinel can distinguish internal short-circuiting from a
user panic, which still becomes `Die`, and Go 1.27 generic methods make
`d.Bind[A](...) A` expressible. Nevertheless it has material costs:

- user `defer` blocks run during every typed short-circuit;
- a broad `recover` inside the callback can accidentally intercept the sentinel;
- stack traces and debugging become less intuitive;
- panic cost is paid on an expected failure path;
- the binder must be runtime-scoped and reject use after the callback returns;
- heterogeneous `R`/`E` composition still needs explicit adaptation.

Therefore direct-style `Bind` should **not** be a foundational API or an internal
runtime mechanism. If demand remains after the safe builder is used in real code,
it may be offered in a clearly documented experimental subpackage, implemented on
top of normal interpretation and tested against sentinel leakage, `recover`,
defer, cancellation and defects.

## 21.4 Keep generation optional, mechanical and boundary-focused

An external source generator could invent bespoke do syntax and emit `FlatMap`,
but it would add a second source language, generated-code navigation problems,
tooling integration and another compatibility surface. Go has no hygienic macro
facility that would make this transparent.

Code generation is useful where the result is a boring, inspectable adapter that
a user could reasonably have written by hand. A later `cmd/effectgen` may opt in
to generating:

- an application-local façade that fixes `R` and `E` once, such as
  `appeffect.Now()` and `appeffect.LogInfo(...)`;
- explicitly configured mappings from base errors such as `IOError` into an
  application's named error type;
- finite-arity conveniences such as `Zip3`/`Zip4` when usage data shows they
  materially improve programs;
- recording or stub adapters for narrow capability ports.

It must not inspect arbitrary domain values, infer mappings by naming convention,
rewrite user function bodies, or generate a hidden panic/coroutine protocol. The
input must declare every cross-boundary mapping. Generated output must:

```text
be ordinary, fully typed Go
be deterministic and gofmt-formatted
carry a generated-code header and generator version
be safe to commit and navigate
have golden tests
be regenerated in CI with a clean-diff check
```

`go generate` is not run automatically by `go build`, so hand-written
`Operations`/`IOOperations` remain the dependable base API. Do not introduce the
generator until at least two real application façades establish the input model;
otherwise it would fossilize guesses about domain error and environment shape.

The core library and its tutorials must remain ordinary valid Go without a
generation step. The recommended order is:

```text
idiomatic combinators
    ↓
    safe typed state builder for longer dependent workflows
    ↓
    optional generated boundary façades where repetition is demonstrated
    ↓
    experimental direct style only if users accept its panic-based semantics
```

---

# 22. Stack safety must be evaluated before freezing internals

The current `Effect` implementation composes nested evaluator closures.

That is elegant and simple, but:

```go
fx.
    FlatMap(...).
    FlatMap(...).
    FlatMap(...)
```

ultimately creates nested Go calls.

Go has dynamically growing goroutine stacks, so this is much less problematic than on many runtimes, but Go does not perform general tail-call optimization and arbitrarily deep effect composition may still become expensive or overflow.

Stress testing of the closure representation found this concrete boundary with
Go 1.27.1:

```text
100,000 sequential Maps    passes
1,000,000 sequential Maps  exceeds Go's 1 GB goroutine-stack limit
```

Keep the 100,000-operation case as an executable regression test. Treat the
million-operation overflow as evidence that the closure evaluator must not be
frozen as the long-term runtime representation.

and benchmark:

- stack growth;
- allocations;
- runtime;
- memory.

Change the **private** effect representation to a small instruction/trampoline
interpreter before declaring the sequential runtime stack-safe. The private
interpreter may need an existential value representation because Go cannot store
a heterogeneous continuation stack; any such use of `any` must remain inside the
interpreter boundary, be justified there, and be guarded by construction so the
public `Effect[R,E,A]` API remains statically typed. Do not work around the limit
with hidden goroutines or a larger configured stack.

Retry, repetition and predicate loops must also use an iterative interpreter loop
or trampoline. They must not add one Go stack frame per attempt, even if ordinary
finite `FlatMap` nesting initially remains closure-based.

That still does **not** imply implementing a scheduler. The trampoline would provide stack-safe effect interpretation while goroutines remain the concurrency runtime.

The public `Effect` representation is already opaque enough that this can remain an implementation decision.

---

# 23. Proposed public runtime surface

The initial target should be roughly:

```go
// Structured lifetime.
func Scoped[R, E, A any](
    f func(Scope) Effect[R, E, A],
) Effect[R, E, A]

// Fiber creation.
func Fork[R, E, A any](fx Effect[R, E, A]) Effect[
    R,
    Never,
    Fiber[E, A],
]

// Fiber observation.
func (fiber Fiber[E, A]) Await() Effect[
    Unit,
    Never,
    Exit[E, A],
]

func (fiber Fiber[E, A]) Join() Effect[
    Unit,
    E,
    A,
]

func (fiber Fiber[E, A]) Interrupt() Effect[
    Unit,
    Never,
    Exit[E, A],
]

func (fiber Fiber[E, A]) Done() <-chan struct{}

// Resource construction.
func (scope Scope) AcquireRelease[R, E, A any](
    acquire Effect[R, E, A],
    release func(A) Effect[R, Never, Unit],
) Effect[R, E, A]

// Parallel composition.
func ZipPar[R, E, A, B any](
    fx Effect[R, E, A],
    that Effect[R, E, B],
) Effect[R, E, Product[A, B]]

// Phantom channel arguments on low-level base effects are normally selected
// once through For[R,E]() or IO()/IOFor[R]().
func Sleep[R, E any](duration time.Duration) Effect[R, E, Unit]

type Operations[R, E any] struct { /* no runtime state */ }
func For[R, E any]() Operations[R, E]

type IOOperations[R any] struct { Operations[R, IOError] }
func IO() IOOperations[Unit]
func IOFor[R any]() IOOperations[R]

func (fx Effect[R, E, A]) Delay(
    duration time.Duration,
) Effect[R, E, A]

func (fx Effect[R, E, A]) Retry[Out any](
    policy Schedule[E, Out],
) Effect[R, E, A]

func (fx Effect[R, E, A]) RetryCause[Out any](
    policy Schedule[Cause[E], Out],
) Effect[R, E, A]

func (fx Effect[R, E, A]) Repeat[Out any](
    policy Schedule[A, Out],
) Effect[R, E, Out]

// Native-channel interop.
func Send[A any](
    ch chan<- A,
    value A,
) Effect[Unit, Never, Unit]

func Recv[A any](
    ch <-chan A,
) Effect[Unit, Never, Receive[A]]
```

Heterogeneous `ZipParMerge`, `FlatMapMerge`, `RetryOrElse`, etc. continue to use
structural `Product`/`Either`.

The typed state builder from section 21 remains a thin convenience surface, not
a runtime primitive. Keep it small (`Do`, `Bind`, `Yield`) until real programs
demonstrate a need for more operations.

---

# 24. Implementation sequence

## Phase 0 — strengthen the existing sequential core

Before concurrency:

1. replace scalar `Cause` with `Empty/Fail/Die/Interrupt/Then/Both`;
2. preserve `context.Cause`, not merely `ctx.Err`;
3. add `FailCause`/internal cause propagation operations;
4. define and document `Never`;
5. add deep-composition tests;
6. add algebra/cause laws and fuzz tests;
7. add the small sequencing conveniences (`As`, `Tap`, `Flatten`, `AndThen`) that
   examples demonstrate are useful;
8. add deterministic human/structured Cause rendering.

**Exit criterion:** sequential error/defect/interruption semantics are lossless enough to support parallelism.

## Phase 1 — runtime and scope

Implement:

1. private runtime state and immutable capability set;
2. live `Clock`, `FileSystem` and `Logger` defaults plus runtime-local overrides;
3. base effect constructors for clock, filesystem and structured logging;
4. no-op/recording observer and initial runtime event model;
5. effect names, spans and annotations;
6. `Scope`;
7. lifecycle state machine;
8. cancellation with causes;
9. child tracking using `WaitGroup.Go`;
10. LIFO finalizers;
11. `Scoped`;
12. scope-close cause composition.

**Exit criterion:** capability overrides are isolated between concurrent runtimes;
base effects remain lazy and typed; observers cannot change application exits; and
scopes cannot leak registered resources or children under success, failure, panic
or cancellation.

## Phase 2 — Fiber

Implement:

1. `FiberID`;
2. stored result + `done` channel;
3. `Fork`;
4. `Await`;
5. `Join`;
6. `Poll`;
7. `Done`;
8. `Interrupt`;
9. fiber lifecycle events and stable IDs.

**Exit criterion:** repeated concurrent `Await`/`Join` calls are race-free and produce the same result, interruption waits for cleanup, and children cannot outlive their scope.

## Phase 3 — resources

Implement:

1. `Scope.AcquireRelease`;
2. finalizer registration race handling;
3. cancellation-independent cleanup context;
4. `Ensuring`;
5. `OnExit`;
6. scoped layer construction;
7. resource lifecycle events without exposing resource values.

**Exit criterion:** successful acquisition always implies exactly-once release, including cancellation races.

## Phase 4 — clock, retry and repetition

Implement:

1. injectable live/test clock boundary;
2. the minimal runtime construction/options needed to inject that clock;
3. interruptible `Sleep` and `Delay`;
4. immutable `Schedule` plus fresh per-run driver;
5. `Stop`, `Forever`, `Recurs`, `Spaced`, capped exponential/Fibonacci
   backoff, elapsed limits, predicates, deterministic jitter and schedule
   intersection/union;
6. `Retry`, `RetryCause`, `RetryN` and `RetryOrElse`;
7. `Repeat`;
8. schedule metadata and retry events;
9. deterministic clock, cancellation and resource-placement tests.

Keep fixed-rate/calendar scheduling and effectful policies deferred until the
basic pure-driver laws are stable in real applications.

**Exit criterion:** one policy is safely reusable by concurrent runs; retry never
retries defects/interruption; all waits are deterministic under the test clock and
interruptible under the live clock.

## Phase 5 — high-level structured concurrency

Implement:

1. `ZipPar`;
2. heterogeneous `ZipParMerge`;
3. `Race`;
4. `RaceFirst`;
5. `AllPar`;
6. bounded parallel traversal;
7. timeout.

**Exit criterion:** no combinator returns while losing/failed children are still executing or finalizing.

## Phase 6 — channel integration

Implement only thin, idiomatic adapters first:

1. `Send`;
2. `Recv`;
3. timer/channel interoperability examples;
4. Fiber `Done()` interoperability examples.

Do **not** yet build Queue/Stream/Hub abstractions.

**Exit criterion:** native channel semantics remain recognizable and cancellation never introduces goroutine leaks.

## Phase 7 — complete Runtime lifecycle and detached work

Only after normal structured concurrency is sound:

```go
Runtime
NewRuntime
Runtime.Run
Runtime.Close
ForkDaemon
```

`ForkDaemon` remains bounded by runtime lifetime. `Runtime.Close` also flushes
owned asynchronous observers/loggers and reports debug-mode fiber/resource leaks.

---

# 25. Required tests

Concurrency code should not be accepted merely because ordinary unit tests pass.

CI should add:

```sh
go test ./...
go test -race ./...
go vet ./...
```

and targeted tests should cover:

- many simultaneous `Await`s on one Fiber;
- many simultaneous `Join`s;
- `Poll` before/after completion;
- parent cancellation;
- explicit fiber interruption;
- cancellation cause preservation;
- child cancellation on scope close;
- interrupt waiting for child finalizers;
- LIFO resource release;
- resources released exactly once;
- acquisition racing scope closure;
- panic in effect -> `Die`;
- panic in finalizer preserved with original cause via `Then`;
- simultaneous parallel failures -> `Both`;
- induced sibling interruption not misreported as an independent failure;
- `Race` first-success semantics;
- `RaceFirst` first-completion semantics;
- channel cancellation;
- receive on closed channel;
- nil channel + cancellation;
- send to closed channel -> defect;
- nested scopes;
- deeply nested fibers;
- deeply composed `FlatMap`;
- all retry/schedule cases listed in section 20.8.

Concurrency stress tests should also be run repeatedly:

```sh
go test -race -count=100 ./...
```

for the lifecycle-heavy packages during development.

## 25.1 Executable example acceptance suite

Every public capability must have at least one minimal example in which the
feature is visibly useful. Focused documentation may also use disconnected API
snippets, but every feature must additionally appear in a complete program whose
logic is invoked by an end-to-end test.

Use a small scenario matrix rather than one artificial mega-example:

| Executable scenario | Capabilities exercised | End-to-end assertions |
|---|---|---|
| Resilient file import | `FileSystem`, `Clock`, `Schedule`, `Retry`, structured `Logger`, span/annotations, observer | transient typed read failures retry with test-clock delays; successful data is written; logs/events contain stable attempt and span metadata |
| Scoped parallel work | `Scope`, acquire/release, `Fork`, `Join`, `ZipPar`, cancellation | results compose; child ownership is visible; release is exactly once and after children; no live fibers/resources remain |
| Failure diagnostics | typed failure, panic defect, interruption, `Then`/`Both`, Cause renderer | complete cause shape and deterministic human/structured rendering are preserved |
| Sequential workflow | ordinary combinators and typed state builder | dependent values compose lazily; failure short-circuits later steps; repeated/concurrent runs do not share builder state |
| Native channel bridge | `Send`, `Recv`, Fiber `Done`, cancellation | native closure semantics remain intact and cancellation leaves no goroutine behind |

An example can cover several capabilities, but no capability is considered done
merely because its constructor has a unit test. The acceptance checklist for each
scenario is:

1. a runnable `main` (or exported `RunExample`) using only public API;
2. a deterministic end-to-end test that composes the same program;
3. assertions on the successful value or complete `Exit`, relevant external
   state, cleanup, and recorded observability;
4. a failure/cancellation path where the capability has meaningful behavior;
5. race-detector coverage for scenarios involving fibers, scopes, clocks,
   observers or mutable test capabilities;
6. concise documentation explaining the scenario without requiring knowledge of
   ZIO or Effect-TS.

Keep example fixtures local and deterministic: no external network, real sleeps,
user home directory, global logger mutation or order-dependent shared files. The
filesystem example uses a test runtime or `t.TempDir`; scheduling uses the manual
clock; recorded logs/events are compared structurally rather than by parsing
formatted text.

---

# 25.2 Queue, Hub, Deferred and Stream

ADDED. Section 15 deferred these deliberately, on the rule that an
effect-specific abstraction is introduced only where it adds semantics native Go
channels genuinely lack. Three of the four now exist because they clear that
bar; this section records what the bar was and what the fourth requires.

## What each one adds that a channel cannot

```text
Deferred   one value every waiter observes, rather than the first receiver
           consuming it
Queue      shutdown safe from any side, a choice of what a full queue does,
           and batched taking
Hub        broadcast to a changing set of subscribers, which a channel cannot
           do at all
```

`Deferred` reuses fiber completion's mechanism rather than reimplementing it: a
write-once result, a closed channel as the broadcast, observation any number of
readers may repeat.

`Queue` keeps every other channel semantic on purpose. Values come out in the
order they went in, and shutdown keeps the backlog so a consumer drains before
it learns the producer is finished, which is the signal a closed channel already
gives. What a full queue does is a required argument, not a default, because the
wrong answer is the usual cause of a stalled or lossy pipeline. The three
answers are separate strategy types, so neither the API nor the implementation
carries a mode flag.

`Hub` gives each subscriber its own inbox, so a slow subscriber's cost is its
own under a dropping policy and the publisher's under a suspending one.
Subscribing is a scoped resource and nothing else: forgetting to unsubscribe is
a leak, because the hub would keep filling an inbox nobody reads.

Batched taking needs two operations, not one. A blocking form waits for a value,
so an empty result means finished; a non-blocking form never waits, so an empty
result means empty right now. Draining a backlog and consuming a stream need
different answers, and one operation giving both the same answer is a hang
waiting to happen.

## Stream representation

The representation must be settled before any combinator is written, because
two of its properties cannot be retrofitted without changing every signature.

**Chunked from the start.** A stream moves `Chunk[A]`, not `A`. Per-value
plumbing costs an interpretation per element, and adding chunks later changes
the type of every transform and sink. `Chunk` is a value type rather than a bare
slice so a source cannot alias a buffer it later reuses, and so the
representation can change without touching callers.

**Pull-based.** A consumer asks for the next step:

```text
Stream[R,E,A]  =  open(Scope) -> Effect[R,E, next]
                  where next : Effect[R,E, Step[A]]
```

`open` acquires the stream's sources in the consumer's scope, so a file or a
subscription lives exactly as long as the stream that reads it. It yields the
effect that produces the next step, and interpreting that effect repeatedly
walks the stream. That effect closes over per-run state and is therefore created
per run, which is what keeps one `Stream` value reusable -- the same rule that
makes a `Schedule` reusable and its driver not.

Pull gives backpressure for free: nothing is produced until a consumer asks.
Push would need somewhere to put values a consumer is not ready for, which means
a buffer with a policy, or a scheduler -- and section 1.1 rules the scheduler
out.

`Step[A]` is a chunk or the end of the stream, and its zero value is the end, so
a source that forgets to say so cannot produce an infinite stream of nothing.

The run loop is a `FlatMap` recursion, so it inherits the interpreter's stack
safety: an infinite stream consumed with `Take` adds no frames per element.

## Deliberately outside the first Stream

```text
merging and interleaving two streams
broadcasting one stream to several consumers through a Hub
grouping, windowing and time-based batching
concurrent per-element effects
pipes or transducers as first-class composable values
```

Each of these is expressible on the representation above, and none of them
constrains it. Building them before there is a consumer would fossilise guesses
about their shape, which is the same reason section 21.4 holds the generator
back.


# 26. Documentation plan

Continue the existing Diátaxis split.

Documentation examples should call or excerpt the executable programs from
section 25.1 so examples and end-to-end tests cannot silently drift apart.

## Tutorials

Add:

```text
tutorials/concurrency.md
tutorials/resources.md
tutorials/retry-and-scheduling.md
```

showing only the happy-path mental model.

## How-to

Add focused operational guides:

```text
how-to/fork-and-join.md
how-to/cancel-work.md
how-to/manage-resources.md
how-to/use-go-channels.md
how-to/adapt-context-apis.md
how-to/run-parallel-work.md
how-to/retry-failures.md
how-to/test-time.md
how-to/write-sequential-workflows.md
```

## Reference

Document exact semantics, especially:

```text
reference/fiber.md
reference/scope.md
reference/cause.md
reference/interruption.md
reference/channels.md
reference/schedule.md
reference/retry.md
```

These should state invariants, not motivate them.

## Explanation

Add:

```text
explanation/structured-concurrency.md
explanation/go-runtime-model.md
explanation/resource-safety.md
explanation/cause-algebra.md
explanation/sequencing-in-go.md
```

The Go runtime explanation should explicitly document where this library intentionally differs from ZIO/Effect rather than suggesting it literally implements their execution model.

---

# 27. Architectural invariants to freeze

The implementation should be considered correct only if these remain true:

1. **No effect executes before interpretation.**
2. **Every library-created goroutine has a lifetime owner.**
3. **Scope closure waits for owned fibers.**
4. **A successfully acquired scoped resource is released exactly once.**
5. **Children terminate before resources belonging to their scope are released.**
6. **`Await`/`Join` are repeatable and multi-consumer.**
7. **Cancellation does not widen `E`.**
8. **Defects do not widen `E`.**
9. **Parallel and sequential failure information is not discarded.**
10. **Typed failures remain distinct from defects and interruption.**
11. **Native Go channels retain native Go closure/ownership semantics.**
12. **No custom scheduler competes with the Go scheduler.**
13. **`context.Context` remains the interoperability boundary for cancellation and deadlines.**
14. **Runtime metadata is not hidden in `context.Value`.**
15. **Detached work is still owned by an explicit runtime.**
16. **Resource and concurrency lifetimes use one coherent Scope mechanism.**
17. **High-level concurrency operators do not return before their discarded children have finalized.**
18. **The public API remains independent of the internal evaluator representation.**
19. **A reusable Schedule contains no per-run mutable driver state.**
20. **Retry never retries defects or interruption unless an explicitly unsafe future operator says so.**
21. **Clock waits are interruptible and testable without wall-clock sleeps.**
22. **Sequencing convenience APIs preserve laziness and the three typed channels.**

---

# 28. Recommended immediate implementation boundary

The next implementation milestone should cover **Phases 0–4**: lossless causes,
Scope/Fiber/resource safety, and the minimal deterministic clock/schedule/retry
slice. Do not include the broad parallel collection API, calendar scheduling,
effectful schedules or detached fibers in that milestone.

In particular, do not start HTTP/schema/ecosystem work yet.

The milestone should demonstrate this resource-safe retry workflow:

```go
attempt := Scoped(func(scope Scope) Effect[Env, AppError, Result] {
    return scope.
        AcquireRelease(acquireResource, releaseResource).
        FlatMap(func(resource Resource) Effect[Env, AppError, Result] {
            return useResource(resource)
        })
})

program := attempt.Retry(
    AndSchedules(
        Recurs[AppError](3),
        Exponential[AppError](100*time.Millisecond, 5*time.Second),
    ),
)
```

with tests proving:

```text
lazy construction
      ↓
immediate first attempt
      ↓
resource acquisition and use
      ↓
typed success/failure/defect/interruption
      ↓
failed-attempt scope and resource cleanup
      ↓
interruptible, test-clock-controlled backoff
      ↓
retry only for typed failure
      ↓
complete Exit
```

Fiber tests in the same milestone must independently prove repeated joins,
interruption and child cleanup. If both slices work correctly under cancellation,
panics, repeated execution and `go test -race`, the runtime architecture is strong
enough to add `ZipPar`, races and collection concurrency.

Exercise the typed state builder against several realistic workflows during this
milestone, comparing its verbosity and allocation cost with ordinary combinators
and small named helper functions before expanding its public surface.
