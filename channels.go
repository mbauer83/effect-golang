package effect

import (
	"context"

	runtimecore "github.com/mbauer83/effect-golang/internal/runtime"
)

// Receive is the result of a channel receive, preserving Go's own two-value
// form: OK is false when the channel was closed and drained.
//
// Closure is not exceptional in Go, so it is reported as a value rather than as
// a typed failure. RecvOrFail is available when an application genuinely treats
// closure as a domain error.
type Receive[A any] struct {
	Value A
	OK    bool
}

// Send sends value on ch, waiting until the send can proceed or the effect is
// interrupted.
//
// Its typed failure channel is Never. Go provides no race-free way to test
// whether another goroutine will close a channel before a send completes, and
// sending on a closed channel is defined to panic, so this reports that panic
// as a defect. Channel closure remains an ownership question: the side
// responsible for closing must not send afterwards.
//
// A nil channel blocks forever on its own, but selecting alongside cancellation
// keeps this effect interruptible, exactly as Go's own select semantics do.
func Send[R, A any](ch chan<- A, value A) Effect[R, Never, Unit] {
	return From(func(ctx context.Context, _ R) Exit[Never, Unit] {
		select {
		case ch <- value:
			return ExitSuccess[Never](Unit{})
		case <-ctx.Done():
			return exitInterrupted[Never, Unit](runtimecore.CancellationReason(ctx))
		}
	})
}

// Recv receives from ch, waiting until a value arrives, the channel is closed,
// or the effect is interrupted.
func Recv[R, A any](ch <-chan A) Effect[R, Never, Receive[A]] {
	return From(func(ctx context.Context, _ R) Exit[Never, Receive[A]] {
		select {
		case value, open := <-ch:
			return ExitSuccess[Never](Receive[A]{Value: value, OK: open})
		case <-ctx.Done():
			return exitInterrupted[Never, Receive[A]](runtimecore.CancellationReason(ctx))
		}
	})
}

// RecvOrFail receives from ch and fails with onClosed when the channel has been
// closed and drained. It is for applications that regard closure as a domain
// failure; Recv preserves Go's own semantics.
func RecvOrFail[R, E, A any](ch <-chan A, onClosed E) Effect[R, E, A] {
	return From(func(ctx context.Context, _ R) Exit[E, A] {
		select {
		case value, open := <-ch:
			if !open {
				return ExitFailure[E, A](onClosed)
			}
			return ExitSuccess[E](value)
		case <-ctx.Done():
			return exitInterrupted[E, A](runtimecore.CancellationReason(ctx))
		}
	})
}

// The carrier forms below select the program's channels once, so a channel
// operation whose own failure channel is Never still composes with failing work.

// Send sends a value on a native channel using these channels.
func (Operations[R, E]) Send[A any](ch chan<- A, value A) Effect[R, E, Unit] {
	return WidenError[E](Send[R](ch, value))
}

// Recv receives from a native channel using these channels, preserving Go's
// closed-channel semantics as a value.
func (Operations[R, E]) Recv[A any](ch <-chan A) Effect[R, E, Receive[A]] {
	return WidenError[E](Recv[R](ch))
}

// RecvOrFail receives from a native channel and fails with onClosed when the
// channel has been closed and drained.
func (Operations[R, E]) RecvOrFail[A any](ch <-chan A, onClosed E) Effect[R, E, A] {
	return RecvOrFail[R](ch, onClosed)
}
