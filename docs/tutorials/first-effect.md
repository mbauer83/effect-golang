# Your first effect

Define the environment, expected failure, and successful value explicitly:

```go
type Config struct {
    Prefix string
}

type LoadError struct {
    Message string
}

load := effect.FromEither(func(ctx context.Context, cfg Config) effect.Either[LoadError, string] {
    return effect.Right[LoadError](cfg.Prefix + "value")
})
```

Transform successful values with `Map`:

```go
program := load.Map(strings.ToUpper)
```

Run only at the application boundary:

```go
exit := effect.Run(context.Background(), Config{Prefix: "item:"}, program)
```

Inspect `Exit` to distinguish success from typed failure, defect, or interruption. Application logic can therefore remain suspended and composable until a runtime boundary is deliberately chosen.
