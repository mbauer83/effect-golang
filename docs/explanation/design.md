# Design and trade-offs

## Why `Either`, not `Or`

The error composition used by `FlatMapChannels` is an exclusive sum: a sequential computation fails at one stage. `Either[L,R]` is therefore the appropriate datatype and is right-biased as a monad for fixed `L`.

An inclusive sum (`These`/`Ior`) is a different algebra and should only be introduced for an operation that can retain values from both sides simultaneously, for example an accumulating parallel combinator.

## Why structural composition

Go 1.27 can express generic methods but cannot compute or normalize arbitrary type-level unions and intersections. This library therefore makes the missing algebra explicit:

```text
R1 + R2  => Product[R1,R2]
E1 + E2  => Either[E1,E2]
```

This is exact and statically checked. It is not canonical: nested products and sums do not automatically reassociate, deduplicate, or commute.

To keep ordinary programs readable, `FlatMap` specializes the common case where `R` and `E` are already equal; the `*Merge` variants are used only when channel composition is required. Type-growing operations such as `Zip` are package functions because Go 1.27.1 rejects generic methods that recursively construct a receiver type argument with an instantiation cycle ([golang/go#80172](https://github.com/golang/go/issues/80172)).

## Why `Cause[E]`

Expected domain failures, programming defects, and cancellation have different recovery semantics. Treating all three as `error` weakens the type contract. `Cause[E]` keeps them separate while `Exit[E,A]` gives the runtime one total result type.

## Why `Layer[RIn,E,ROut]`

A layer is not a dynamic service locator. It is a typed effect that constructs an environment. Provisioning therefore remains explicit, composable, and checked by the compiler.

## Boundaries

Structural composition is most useful inside implementations. Public module boundaries should generally expose intentional named environment and error contracts and adapt internal structural trees to them. Go cannot perform that normalization generically; making the boundary explicit is preferable to reflection or type erasure.
