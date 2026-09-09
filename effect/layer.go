package effect

import (
	"context"

	runtimecore "github.com/mbauer83/effect-golang/effect/internal/runtime"
)

// Layer describes effectful construction of ROut from RIn. Layer failures are
// typed independently from the effects that consume the produced environment.
type Layer[RIn, E, ROut any] struct {
	build Effect[RIn, E, ROut]
}

// LayerFromEffect lifts an Effect into a Layer.
func LayerFromEffect[RIn, E, ROut any](build Effect[RIn, E, ROut]) Layer[RIn, E, ROut] {
	return Layer[RIn, E, ROut]{build: build}
}

// LayerScoped lifts a resourceful construction workflow into a Layer.
//
// The scope handed to build belongs to the effect this layer is provided to, so
// a service the layer acquires lives exactly as long as its consumer instead of
// being released the moment construction finishes. Layers therefore use the
// same single lifetime mechanism as fibers and ordinary scoped resources.
func LayerScoped[RIn, E, ROut any](build func(Scope) Effect[RIn, E, ROut]) Layer[RIn, E, ROut] {
	return LayerFromEffect(usingCurrentScope(build))
}

// usingCurrentScope hands the ambient dynamic scope to use. It is how a layer
// registers a finalizer in its consumer's lifetime rather than in one of its
// own that would already have closed.
func usingCurrentScope[R, E, A any](use func(Scope) Effect[R, E, A]) Effect[R, E, A] {
	return suspendRuntime(func(_ context.Context, state *runtimecore.State, _ R) Effect[R, E, A] {
		return use(Scope{state: state.Scope()})
	})
}

// Build returns the Effect represented by the Layer.
func (layer Layer[RIn, E, ROut]) Build() Effect[RIn, E, ROut] {
	return layer.build
}

// MapError adapts a Layer's construction failure.
//
// What lets a layer built from something with a failure type of its own be
// provided to a program with a failure type of its own: a settings layer fails
// with a ConfigError, and an application whose failures are its own refusals
// adapts it once, where the layer is assembled, rather than everywhere it is
// used.
//
//	settings := effect.ConfigLayer[Unit](described).
//	    MapError(func(failure effect.ConfigError) Refusal {
//	        return Refusal{Because: failure.Error()}
//	    })
//
// Without this, ProvideLayerSame is unusable for any layer whose failures are
// not already the consumer's, and ProvideLayer answers with an Either the
// program then has to fold at every call.
func (layer Layer[RIn, E, ROut]) MapError[E2 any](adapt func(E) E2) Layer[RIn, E2, ROut] {
	return LayerFromEffect(layer.build.MapError(adapt))
}

// Map transforms the environment produced by a Layer.
func (layer Layer[RIn, E, ROut]) Map[ROut2 any](f func(ROut) ROut2) Layer[RIn, E, ROut2] {
	return LayerFromEffect(layer.build.Map(f))
}

// ThenLayers feeds the first Layer's output into the next Layer. It is a
// package function because its result structurally grows Layer's error type.
func ThenLayers[RIn, E, ROut, E2, ROut2 any](
	layer Layer[RIn, E, ROut],
	next Layer[ROut, E2, ROut2],
) Layer[RIn, Either[E, E2], ROut2] {
	return LayerFromEffect(layer.build.MapError(Left[E, E2]).FlatMap(
		func(provided ROut) Effect[RIn, Either[E, E2], ROut2] {
			return next.build.MapError(Right[E, E2]).ContramapEnv(constantEnvironment[RIn](provided))
		},
	))
}

// ZipLayers combines independent Layers. Inputs, failures, and outputs remain
// exact. The type-growing operation is a package function to avoid a generic
// method instantiation cycle.
func ZipLayers[RIn, E, ROut, RIn2, E2, ROut2 any](
	layer Layer[RIn, E, ROut],
	that Layer[RIn2, E2, ROut2],
) Layer[Product[RIn, RIn2], Either[E, E2], Product[ROut, ROut2]] {
	return LayerFromEffect(ZipMerge(layer.build, that.build))
}

// ProvideLayer satisfies an Effect's entire R channel from a Layer. Layer and
// effect failures remain distinguishable as Left and Right respectively.
//
// Provisioning opens a lifetime that closes only after fx has finished, so a
// resourceful layer's finalizers run once the service is genuinely no longer in
// use rather than when construction returned.
func ProvideLayer[R, E, A, RIn, LE any](fx Effect[R, E, A], layer Layer[RIn, LE, R]) Effect[RIn, Either[LE, E], A] {
	return Scoped(func(Scope) Effect[RIn, Either[LE, E], A] {
		return layer.build.MapError(Left[LE, E]).FlatMap(func(provided R) Effect[RIn, Either[LE, E], A] {
			return fx.MapError(Right[LE, E]).ContramapEnv(constantEnvironment[RIn](provided))
		})
	})
}

// ProvideLayerSame is the common-case specialization when Layer and Effect use
// the same E channel. It avoids producing a redundant Either[E,E] and gives the
// layer's resources the same consumer lifetime as ProvideLayer.
func (fx Effect[R, E, A]) ProvideLayerSame[RIn any](layer Layer[RIn, E, R]) Effect[RIn, E, A] {
	return Scoped(func(Scope) Effect[RIn, E, A] {
		return layer.build.FlatMap(func(provided R) Effect[RIn, E, A] {
			return fx.ContramapEnv(constantEnvironment[RIn](provided))
		})
	})
}
