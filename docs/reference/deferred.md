# Deferred reference

Reach for a native channel first. These types exist only where a channel
genuinely cannot do the job, which is the rule
[the channel reference](channels.md) states and these pages continue.

`Deferred[E, A]` is a value supplied once and observed any number of times.

A channel hands its value to whoever receives first. A `Deferred` stores its
outcome and broadcasts, so every waiter sees the same result. Fiber completion
has the same shape and shares the same mechanism.

```go
func NewDeferred[R, E, A any]() Effect[R, Never, Deferred[E, A]]

func (deferred Deferred[E, A]) Succeed[R any](value A) Effect[R, Never, bool]
func (deferred Deferred[E, A]) Fail[R any](failure E) Effect[R, Never, bool]
func (deferred Deferred[E, A]) Complete[R any](exit Exit[E, A]) Effect[R, Never, bool]
func (deferred Deferred[E, A]) Await[R any]() Effect[R, E, A]
func (deferred Deferred[E, A]) Poll() (Exit[E, A], bool)
func (deferred Deferred[E, A]) Done() <-chan struct{}
```

- It is created by an effect, so it cannot exist before interpretation and
  cannot be shared accidentally between runs.
- Fulfilment happens once. The `bool` reports whether this call was the one that
  fulfilled it, so a caller can tell whether it won the race.
- `Complete` takes a whole `Exit`, which is how a defect or an interruption
  reaches the waiters instead of being lost.
- `Await` adopts the outcome exactly as `Fiber.Join` does, and waiting is
  itself interruptible.
- `Done` is a synchronization signal for an ordinary `select`; read the value
  with `Poll`.
