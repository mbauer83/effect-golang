# Interruption reference

Cancellation is cooperative. `context.Context` is the interoperability
boundary, and the runtime cannot forcibly stop arbitrary Go code.

## Guarantees

1. Built-in combinators observe cancellation.
2. Channel and clock effects `select` on `ctx.Done()`.
3. Adapters propagate the context into context-aware Go APIs.
4. Cancellation is checked at every instruction that settles a value, which is
   every point at which a program produces one.
5. A user callback that never observes its context is responsible for
   cooperating; `CheckInterrupt()` is the smallest checkpoint that can be
   inserted into a sequence.

## Causes

The runtime cancels with an explicit reason, so an interruption records why the
work stopped:

| Reason | Meaning |
|---|---|
| `ErrScopeClosed` | the scope owning the work was closed |
| `ErrFiberInterrupted` | a fiber was interrupted explicitly |
| `ErrSiblingFailed` | a parallel sibling failed, making this branch unnecessary |
| `ErrRaceLost` | another branch completed first |
| `ErrTimedOut` | a timeout elapsed |
| `ErrRuntimeClosed` | the owning `Runtime` was closed |

Compare them with `errors.Is` on an `Interruption`'s `Cause`. A caller's own
`context.WithCancelCause` reason is preserved unchanged.

## Induced interruption

A branch canceled only because this composition no longer needs it did not fail
on its own account. `ZipPar`, `Race`, `RaceFirst`, `Timeout` and the parallel
traversals drop such an interruption instead of reporting it as an independent
failure. Anything else the abandoned branch reported, such as a finalizer
defect, is still composed into the result.

## Cancellation does not widen E

Interruption lives in `Cause`, never in `E`. Neither does a defect. A canceled
effect's `Exit` is a failure whose cause is `Interrupt`, and `IsInterruptedOnly`
distinguishes it from a domain failure.

## Cleanup

Finalizers run with `context.WithoutCancel`, so cancellation cannot skip them.
An uncooperative finalizer can therefore block indefinitely; the runtime favours
releasing resources over truncating cleanup.
