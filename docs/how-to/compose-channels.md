# Compose different channels without widening

Use ordinary `FlatMap` when both effects already share the same `R` and `E`:

```go
first.FlatMap(func(a A) effect.Effect[Env, AppError, B] {
    return second(a)
})
```

This preserves the monadic shape `Effect[Env, AppError, _]`.

Use `FlatMapMerge` when the channels differ:

```go
loadUser.FlatMapMerge(sendMail)
```

Given:

```text
Effect[DB,     DBError,   User]
Effect[Mailer, MailError, Receipt]
```

the result is exactly:

```text
Effect[Product[DB, Mailer], Either[DBError, MailError], Receipt]
```

No requirement or failure type is erased. The trade-off is that Go cannot normalize associative, commutative, or idempotent type expressions automatically. Prefer fixed-channel composition inside a module and expose deliberate named contracts at module boundaries.
