# Cause reference

`Cause[E]` is the complete reason an effect terminated unsuccessfully.

```text
Cause[E] =
    Empty
  | Fail(E)
  | Die(Defect)
  | Interrupt(Interruption)
  | Then(Cause[E], Cause[E])
  | Both(Cause[E], Cause[E])
```

`Then` is sequential composition: an operation failed, then its cleanup failed.
`Both` is independent composition: two parallel branches failed on their own
account. `Empty` is the identity of both and is the zero value.

## Construction

`EmptyCause`, `FailCause`, `DieCause`, `InterruptCause`, and the `Then` and
`Both` methods. `Then` and `Both` collapse an empty operand.

## Inspection

Leaf accessors return a value only when the cause is exactly that one leaf:
`Failure`, `Defect`, `Interruption`. Collectors keep the tree intact and return
every leaf left to right: `Failures`, `Defects`, `Interruptions`.

Predicates: `IsEmpty`, `IsFailureOnly`, `IsInterruptedOnly`, `ContainsDefect`.

`Fold(CauseFolder[E,A])` is total typed elimination. It is iterative, so an
arbitrarily deep cause is safe to eliminate.

`Status()` reduces a cause to the bounded vocabulary runtime events and metric
labels may carry. A defect outranks an interruption.

## Rendering

`String()` renders the tree without panic stacks. `Format` adds them under
`%+v`. Rendering is iterative and total: an empty cause, a nil wrapped error and
a non-string panic value all render.

`Report()` produces `CauseReport`, a structured tree of `Kind`, `Detail`,
`Stack` and `Children` for logs and exporters. No question about an outcome
requires parsing display text.

## Interruption

An `Interruption` carries the cancellation cause, read with `context.Cause`
rather than `ctx.Err`, so a caller's reason survives instead of a generic
`context.Canceled`. See [interruption](interruption.md).

## Conversion

`MapFailure` rewrites every `Fail` leaf and preserves structure.
`OrDie(fx)` rewrites an effect's typed failures as defects for use
where the failure channel must be `Never`.

## Exit

`Exit[E,A]` is either a `Cause[E]` or a successful `A`. Its zero value is an
unsuccessful termination with the empty cause. `Fold`, `Map`, `MapError`,
`Value`, `Cause` and `String` are total.
