package effect

import "context"

// Layer describes effectful construction of ROut from RIn. Layer failures are
// typed independently from the effects that consume the produced environment.
type Layer[RIn, E, ROut any] struct {
	build Effect[RIn, E, ROut]
}

// LayerFromEffect lifts an Effect into a Layer.
func LayerFromEffect[RIn, E, ROut any](build Effect[RIn, E, ROut]) Layer[RIn, E, ROut] {
	return Layer[RIn, E, ROut]{build: build}
}

// Build returns the Effect represented by the Layer.
func (layer Layer[RIn, E, ROut]) Build() Effect[RIn, E, ROut] {
	return layer.build
}

// Map transforms the environment produced by a Layer.
func (layer Layer[RIn, E, ROut]) Map[ROut2 any](f func(ROut) ROut2) Layer[RIn, E, ROut2] {
	return LayerFromEffect(layer.build.Map(f))
}

// Then feeds this Layer's output into the next Layer. Only failure channels
// need composition; the intermediate environment is provided directly.
func (layer Layer[RIn, E, ROut]) Then[E2, ROut2 any](next Layer[ROut, E2, ROut2]) Layer[RIn, Either[E, E2], ROut2] {
	return LayerFromEffect(From(func(ctx context.Context, env RIn) Exit[Either[E, E2], ROut2] {
		first := layer.build.run(ctx, env).MapError(func(failure E) Either[E, E2] {
			return Left[E, E2](failure)
		})
		if cause, ok := first.Cause(); ok {
			return exitCause[Either[E, E2], ROut2](cause)
		}

		provided, _ := first.Value()
		return next.build.run(ctx, provided).MapError(func(failure E2) Either[E, E2] {
			return Right[E](failure)
		})
	}))
}

// Zip combines independent Layers. Inputs, failures, and outputs remain exact.
func (layer Layer[RIn, E, ROut]) Zip[RIn2, E2, ROut2 any](that Layer[RIn2, E2, ROut2]) Layer[Product[RIn, RIn2], Either[E, E2], Product[ROut, ROut2]] {
	return LayerFromEffect(layer.build.ZipMerge(that.build))
}

// ProvideLayer satisfies an Effect's entire R channel from a Layer. Layer and
// effect failures remain distinguishable as Left and Right respectively.
func (fx Effect[R, E, A]) ProvideLayer[RIn, LE any](layer Layer[RIn, LE, R]) Effect[RIn, Either[LE, E], A] {
	return From(func(ctx context.Context, env RIn) Exit[Either[LE, E], A] {
		built := layer.build.run(ctx, env).MapError(func(failure LE) Either[LE, E] {
			return Left[LE, E](failure)
		})
		if cause, ok := built.Cause(); ok {
			return exitCause[Either[LE, E], A](cause)
		}

		provided, _ := built.Value()
		return fx.run(ctx, provided).MapError(func(failure E) Either[LE, E] {
			return Right[LE](failure)
		})
	})
}

// ProvideLayerSame is the common-case specialization when Layer and Effect use
// the same E channel. It avoids producing a redundant Either[E,E].
func (fx Effect[R, E, A]) ProvideLayerSame[RIn any](layer Layer[RIn, E, R]) Effect[RIn, E, A] {
	return From(func(ctx context.Context, env RIn) Exit[E, A] {
		built := layer.build.run(ctx, env)
		if cause, ok := built.Cause(); ok {
			return exitCause[E, A](cause)
		}
		provided, _ := built.Value()
		return fx.run(ctx, provided)
	})
}
