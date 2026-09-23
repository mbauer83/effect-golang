# Sequencing in Go, and the limits of sugar

Long `FlatMap` chains are a real usability problem, especially when a later step
depends on several earlier values. It is worth being precise about why Go cannot
simply borrow the usual answers.

## What the other runtimes rely on

ZIO's `for` comprehension is language-level desugaring. Effect's `yield*` needs
resumable generators. A Go library can add neither.

Go's range-over-function iterators are not an equivalent mechanism. Their
`yield` callback sends values *from* the iterator *to* the loop; the loop cannot
resume the producer with an arbitrarily typed result. They also require one
homogeneous iteration type. `iter.Pull` does resume a coroutine, and was
measured as the basis for direct style and rejected: it passes a
`runtime.Goexit` in the producer through to whoever called `next`, which is the
goroutine running the fiber.

## First remove the dependency

Most long chains are not actually dependent. Before reaching for syntax:

- `Map`, `Zip` and `ZipPar` for independent work;
- `All`, `ForEach` and their parallel variants for collections;
- `As`, `Tap`, `Flatten` and `AndThen` for common shapes;
- small named functions for meaningful workflow stages.

Go does not infer generic arguments from an expected result type, so a
zero-argument primitive such as `Now[R, E]()` cannot infer either phantom
channel. Rather than repeating those arguments throughout a program, select them
once:

```go
operations := effect.For[Env, AppError]()
now := operations.Now()

io := effect.IO() // IOOperations[Unit], error channel IOError
loaded := effect.Zip(io.ReadFile(path), io.Now())
```

`Operations[R,E]` holds no runtime state. It carries compile-time channel
evidence into ordinary constructors, which is why it can also supply the
pre-widened fiber and channel operations whose own failure channel is `Never`.

## Then flatten the layout

`Workflow` packages successive `FlatMap` steps around one caller-declared state
type. It is the closest Go analogue to Effect's non-generator `Do`/`bind`:
TypeScript can grow a structural record after every bind, Go cannot, so one
explicit state type carries the whole workflow.

It is a convenience over the core algebra, not a runtime mode. Failures,
defects, cancellation and stack-safety behave identically. The state factory
runs once per interpretation, so a retry or a concurrent run never inherits
another run's partial state. See the
[checkout example](../../examples/checkout/program.go).

It trades indentation for an explicit state struct. That is a good trade for a
workflow with several cross-step dependencies and a poor one for a two-step
composition.

## Direct style, and what it actually costs

`customer := do.Await(loadCustomer(id))` is what `experimental/direct` offers.
`Await` must return an `A` while also abandoning the body when the effect did not
succeed, and Go has no resumable suspension, so the body runs on a goroutine of
its own in lock step with the interpretation, and a failed `Await` ends that
goroutine with `runtime.Goexit`.

It replaced an implementation that ended the body with a private panic, and the
difference is soundness, not taste:

- a `recover()` in the body could swallow the panic. That was detected after the
  fact and reported as a defect, but it could not be prevented. `recover()` does
  not see a `Goexit`, so there is nothing to swallow;
- a `defer` in the body ran on every expected failure, which was listed as a
  hazard. It is the right behaviour: a `Goexit` runs deferred calls, and a
  `defer` is a finalizer, as a `finally` block is in Effect's `gen`;
- recursion through the panic design grew one Go stack, and at a hundred
  thousand levels the process died of `fatal error: stack overflow`, which no
  `recover` catches.

### What it measures

Three-step dependent workflow, same steps, three styles
(`test/benchmark`, Go 1.27.1, i7-13700H, 20 threads):

| Style | Success | Failure |
|---|---|---|
| `FlatMap` | 460 ns, 10 allocs | 990 ns, 14 allocs |
| `Workflow` | 870 ns, 24 allocs | 1420 ns, 23 allocs |
| `direct` | 1900 ns, 13 allocs | 5400 ns, 18 allocs |

The steps are `Succeed`, so these numbers are almost all framework overhead.
Direct style's is one goroutine hand-off per run, whatever the number of
awaits, and bodies run on parked workers so that the hand-off does not also pay
for a new goroutine growing its stack. A failed run ends its worker, so it pays
for a new one. About half of either path is the scheduler waking an idle thread,
which an idle twenty-thread machine maximises and a loaded one mostly does not.

At scale, with every fiber parked inside its program — a hundred thousand
requests each waiting on I/O — a fiber costs 17 KB written with `FlatMap` and
26 KB in direct style, whose fiber holds two goroutines: its own and its body's.

Recursion through the program itself is where the approaches differ in kind:

| Depth | `FlatMap` | `direct` |
|---|---|---|
| 100k | 21 ms, 7 MB | 683 ms, 901 MB |
| 1M | 231 ms, 83 MB | 30 s, 9 GB |

`FlatMap` is stack-safe because the interpreter keeps its own continuation
stack. A body awaiting a body holds a goroutine per level, so recursion belongs
in a `for` loop inside one body.

### What that changes, and what it does not

The recommendation is direct style for a dependent sequence. The cost is real
and small: a microsecond or two per run is noise against a query, a file or a
request, and it is not paid per step. Where it is not noise — an effect run per
element of a hot stream — write `FlatMap`, which is what the other two compile
down to anyway.

`Workflow` remains for now. Its two lambdas per step are the price of an
explicit state type, which is the thing direct style removes.

## Where a source generator belongs

A generator that invents do-syntax would add a second source language, and Go
has no hygienic macro facility to make it transparent. That is still true, and
none is planned.

Rewriting direct style is a different proposition, because the source is
already ordinary Go that compiles and runs correctly without any generator. A
rewrite into `FlatMap` chains is then an optimisation and not a semantics, and
[`effectgo`](../how-to/rewrite-direct-style.md) is that rewrite:
`go build -overlay` substitutes rewritten files without touching the tree,
`//line` directives keep positions pointing at the source, and a body the
rewriter cannot translate is left alone. Running a test suite both ways is what
checks the claim, and this repository's CI does.

| Style | Success | Failure |
|---|---|---|
| `FlatMap` | 490 ns, 10 allocs | 930 ns, 14 allocs |
| `direct` | 1860 ns, 13 allocs | 4890 ns, 19 allocs |
| `direct`, rewritten | 620 ns, 17 allocs | 1150 ns, 18 allocs |

A rewritten body is a `FlatMap` chain, so it is stack-safe and holds no
goroutine of its own.

Elsewhere, generation is useful where the output is a boring adapter a person
could have written: an application façade that fixes `R` and `E` once, a
declared error mapping, a stub for a narrow port. It must not infer mappings by
naming convention. `go generate` is not run by `go build`, so hand-written
`Operations` remains the dependable base API, and the library and its tutorials
stay ordinary Go with no generation step.
