# Why a cause algebra

A scalar failure type is sufficient only while execution is sequential. It
becomes lossy the moment:

- both parallel branches fail;
- an effect fails and its finalizer also defects;
- several finalizers defect;
- nested concurrent scopes fail in different ways.

Each of those is a real production incident in which the discarded fact was the
interesting one.

## The shape

```text
Cause[E] =
    Empty | Fail(E) | Die(Defect) | Interrupt(Interruption)
  | Then(Cause[E], Cause[E])
  | Both(Cause[E], Cause[E])
```

`Then` and `Both` are not decoration. "The write failed **and then** rollback
failed" and "two independent shards failed" call for different operational
responses, and a flat list of errors cannot distinguish them.

`Empty` makes the zero value meaningful and gives composition an identity, so
`Then` and `Both` need no special cases at call sites.

## Three kinds of bad, kept apart

| Kind | Meaning | Retryable | Lives in |
|---|---|---|---|
| `Fail(E)` | an expected domain outcome | yes | `Cause`, typed as `E` |
| `Die(Defect)` | a programming error, captured with its stack | no | `Cause` only |
| `Interrupt` | work was asked to stop, with the reason | no | `Cause` only |

Collapsing these into one `error` weakens the contract that makes typed effects
worth having. Neither a defect nor an interruption widens `E`, so a function's
signature keeps meaning what it says.

## Interruption keeps its reason

An `Interruption` stores `context.Cause(ctx)`, not `ctx.Err()`.
`WithCancelCause` exists precisely so the underlying reason survives, and
throwing it away turns every stop into an indistinguishable `context.Canceled`.
The runtime attaches its own reasons for the same purpose; see
[interruption](../reference/interruption.md).

## Representation

The public `Cause[E]` is a typed view over one opaque tagged node, not an
exported forest of variant structs. Construction stays controlled, so an invalid
state is not normally reachable, and the representation can change without
breaking callers.

Every operation over it — `Fold`, `MapFailure`, `Report`, `String` — is
iterative rather than recursive. A cause tree can be as deep as a chain of
finalizer failures, and inspecting a failure must not itself fail.

## Total rendering

Formatting is total by construction: an empty cause, a nil wrapped error, a
non-string panic value and a partially built tree all render. A renderer that
panics while reporting a panic is worse than no renderer.

Two renderings exist because two audiences do: `String` for a human, `Report`
for a log or exporter. Neither parses the other, and no question about an
outcome needs display text to answer it.
