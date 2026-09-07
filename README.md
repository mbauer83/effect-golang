# effect-golang

A small, typed effect system for Go 1.27+.

```go
Effect[R, E, A]
```

means: a suspended computation that requires `R`, may fail with an expected `E`, and may succeed with `A`.

The core design preserves all three channels during composition:

- requirements compose as `Product[R1, R2]`;
- expected failures compose as `Either[E1, E2]`;
- values remain fully typed, using `Product[A, B]` where both are retained.

Panics and cancellation are not smuggled into `E`: `Run` returns `Exit[E, A]`, whose failure side is a `Cause[E]` distinguishing typed failures, defects, and interruption.

Go 1.27 is required because the public API relies on generic methods.

## Documentation

- [Tutorial: first effect](docs/tutorials/first-effect.md)
- [How-to: compose different channels](docs/how-to/compose-channels.md)
- [Reference: core model](docs/reference/core.md)
- [Explanation: design and trade-offs](docs/explanation/design.md)

The project is intentionally focused on the core algebra and runtime semantics first. Ecosystem integrations will be evaluated separately for adoption, usability, security, documentation, maintenance, and long-term compatibility.
