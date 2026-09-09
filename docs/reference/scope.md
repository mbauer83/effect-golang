# Scope reference

`Scope` is a handle to one lifetime boundary. It owns the fibers started inside
it and the finalizers of the resources acquired inside it. Copying a `Scope` is
cheap; its synchronization state stays behind a pointer and is never copied.

`Scope` exposes no `Close`. The code that created a scope owns its closure.

## Creation

`Scoped(use func(Scope) Effect[R,E,A]) Effect[R,E,A]` opens a scope, hands it to
`use`, and closes it when the resulting effect settles.

`Scope` is a lexical parameter, not part of `R`. A structural `Product`
environment cannot subtract a requirement once composed, so a scope in `R` would
appear in the public type of every resource-using effect.

## Closure

Closing a scope, in order:

1. rejects further registration;
2. cancels the context shared by everything the scope owns;
3. waits for every owned fiber to terminate;
4. runs finalizers in reverse acquisition order;
5. returns the composed cleanup cause.

Step 3 precedes step 4 because a child may still be using a resource its scope
acquired.

Finalizers run with a context detached from cancellation via
`context.WithoutCancel`, so an already-canceled caller cannot skip cleanup. That
context keeps its values and loses its deadline.

A cleanup cause is appended to the body's own cause with `Then`. A finalizer
defect therefore turns a successful body into a failure rather than being
discarded, and never replaces the body's failure.

## Resources

`scope.AcquireRelease(acquire, release)` acquires a resource and registers its
release.

- Registration is atomic with acquisition: a resource produced by `acquire` is
  owned by the scope before the runtime reaches another interruption
  checkpoint.
- If `acquire` is interrupted before producing a resource, cleanup of partial
  state belongs to `acquire`, which is the conventional contract of a
  context-aware Go API.
- Release runs exactly once per successful acquisition.
- Acquiring into a scope that has already begun closing releases the resource
  immediately and fails with an interruption naming `ErrScopeClosed`.

`release` has a `Never` failure channel: one scope may hold unrelated resources
whose release errors share no type, and those types cannot be added to `E` after
the effects have been composed. Three honest conversions exist:

- `Release(func(context.Context) error)` turns a conventional close error into a
  defect;
- `OrDie(fx)` does the same for an existing effect;
- handling the error before registration keeps it in `E`.

## Fibers

`scope.Fork(fx)` starts work owned by this scope rather than by the current
dynamic scope. `Fork(fx)` uses the current dynamic scope. `ForkDaemon(fx)` uses
the `Runtime` root scope.

Forking into a scope that has begun closing starts no work and fails with an
interruption naming `ErrScopeClosed`.

## Cleanup without a scope

`fx.Ensuring(finalize)` and `fx.OnExit(finalize)` attach cleanup to one effect
rather than to a lifetime. They obey the same rules: `Never` failure channel,
detached cleanup context, and `Then` composition of causes. `OnExit` also
receives the `Exit` being finalized.

## Layers

`LayerScoped(build func(Scope) Effect[RIn,E,ROut])` receives the scope of the
effect the layer is provided to, so a resourceful service lives as long as its
consumer. `ProvideLayer` and `ProvideLayerSame` open that scope.
