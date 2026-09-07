package effect

import (
	"context"
	"fmt"
	"time"

	runtimecore "github.com/mbauer83/effect-golang/effect/internal/runtime"
)

// Now reads the current runtime clock. R and E are phantom channels that let
// this infallible capability compose directly with an application effect.
func Now[R, E any]() Effect[R, E, time.Time] {
	return fromRuntime(func(_ context.Context, state *runtimecore.State, _ R) Exit[E, time.Time] {
		return ExitSuccess[E](state.Capabilities().Clock.Now())
	})
}

// Sleep waits on the runtime clock and remains cooperatively interruptible.
// R and E are phantom channels: a wait cannot fail with a domain error.
//
// A clock whose wait reports an infrastructure failure is a broken capability
// rather than an application failure, so it becomes a defect.
func Sleep[R, E any](duration time.Duration) Effect[R, E, Unit] {
	return fromRuntime(func(ctx context.Context, state *runtimecore.State, _ R) Exit[E, Unit] {
		err := state.Capabilities().Clock.Sleep(ctx, duration)
		if exit, interrupted := interruptedExit[E, Unit](ctx); interrupted {
			return exit
		}
		if err != nil {
			panic(fmt.Errorf("effect: Clock.Sleep failed: %w", err))
		}
		return ExitSuccess[E](Unit{})
	})
}

// Operations carries these channels into the operations below, whose own
// requirement channel is unused and whose failure channel is Never. Selecting
// the channels once keeps a program composable with FlatMap instead of forcing
// a widening at every step. The precise, narrower forms remain available.

// Now reads the runtime clock using these channels.
func (Operations[R, E]) Now() Effect[R, E, time.Time] {
	return Now[R, E]()
}

// Sleep waits on the runtime clock using these channels.
func (Operations[R, E]) Sleep(duration time.Duration) Effect[R, E, Unit] {
	return Sleep[R, E](duration)
}
