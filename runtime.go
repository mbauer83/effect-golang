package effect

import "context"

// Run evaluates fx with env. Expected failures, defects, and interruption are
// all returned as Exit; Run does not panic for defects raised by the effect.
func Run[R, E, A any](ctx context.Context, env R, fx Effect[R, E, A]) Exit[E, A] {
	return fx.run(ctx, env)
}
