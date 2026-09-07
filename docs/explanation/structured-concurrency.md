# Structured concurrency

Every concurrent operation in this runtime answers one question first: **who
owns this work, and when does that owner stop waiting?**

## One mechanism

`Scope` is the only lifetime mechanism. It powers fibers, resources, layer
resources and explicit finalizers. There is no second cleanup system with
subtly different rules.

```text
scope
├── fiber A
│   └── scope A
│       ├── fiber A1
│       └── resources
└── fiber B
    └── scope B
```

A child created through the effect API cannot silently become an orphan. Closing
a scope cancels its children, waits for all of them, and only then releases the
resources they might have been using.

## What a combinator promises

No high-level operator returns while work it discarded is still executing or
finalizing. That is a stronger promise than "the result is available", and it is
what makes a timeout safe to use around a database transaction.

`ZipPar` forks both branches into a private lifetime. When one branch fails, the
other is canceled *and awaited*. `Race` waits for the first **success**, so one
branch's failure does not end the race. `RaceFirst` lets the first **completion**
win, which is the direct analogue of selecting on two completion channels. Having
both avoids an ambiguous `Race`.

`Timeout` is a race against the runtime clock and uses the same algorithm, so it
cannot return while the timed-out work is still running.

## Failure is not reduced

Two branches that fail on their own account compose with `Both`, in positional
order. A branch canceled only because its sibling failed did not fail
independently, and is not reported as though it had. Anything else that branch
reported on the way out, such as a finalizer defect, is still composed in.

```text
Both(
  Then(Fail(validate), Die(cleanup)),
  Then(Die(panic),     Die(cleanup)),
)
```

Four facts. A scalar error model keeps one. The
[diagnostics example](../../examples/diagnostics/program.go) produces exactly
this cause and asserts on it.

## Bounded parallelism

`ForEachParN` bounds simultaneous branches with a buffered channel used as a
semaphore. Each branch stays one ordinary goroutine owned by the private scope,
and a branch waiting for a slot remains cancelable. Results are collected in
input order however the branches interleave.

## Detached work is still owned

`ForkDaemon` attaches to the `Runtime` root scope. A daemon outlives the `Run`
that created it and is bounded by `Runtime.Close`, which interrupts and awaits
it. There is no genuinely unowned work.
