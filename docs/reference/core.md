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
- `FlatMapMerge`: sequence effects with different channels using `Product` and `Either`.
- `Zip` / `ZipMerge`: retain both successful values.
- `CatchAll`: handle every typed `E`; defects and interruption remain unhandled.

## Exit and Cause

`Run` returns `Exit[E,A]`.

A failed exit contains one `Cause[E]`:

- `CauseFailure`: expected typed `E`;
- `CauseDefect`: panic captured with a stack;
- `CauseInterrupted`: context cancellation or deadline.

## Layer

`Layer[RIn,E,ROut]` is effectful construction of an environment.

- `Then` feeds one layer's output into another.
- `Zip` builds independent layers and retains both outputs.
- `ProvideLayer` satisfies an effect's `R` and keeps layer/effect failures distinguishable.
- `ProvideLayerSame` avoids a redundant `Either[E,E]` when both use the same error channel.
