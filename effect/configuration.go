package effect

// Reading a program's settings as an effect.
//
// The description language is in effect/config, which knows nothing about
// effects: it composes descriptions and reads them from a source. This is the
// three lines that make reading one an effect -- so settings arrive through the
// same failure channel, the same interruption and the same layers as anything
// else a program does, and so nobody has to thread a settings argument down to
// whoever needs a value.

import (
	"context"

	"github.com/mbauer83/effect-golang/effect/config"
	runtimecore "github.com/mbauer83/effect-golang/effect/internal/runtime"
)

// ConfigSource is the runtime port a program's settings are read through.
type ConfigSource = config.Source

// ConfigError is why a description could not be read. It is compositional: a
// program missing two settings is told about two settings.
type ConfigError = config.Error

// LoadConfig reads a description through the runtime's configuration source.
//
// A typed failure and not a defect. A deployment that has not been told the
// address of its database is a normal, expected, recoverable thing that a
// program may want to report, retry or fall back from -- and a program that
// would rather not can turn it into a defect with OrDie, which is a decision
// its author makes rather than one this makes for them.
//
//	settings := effect.LoadConfig[Env](describedSettings)
//
// Read when the effect is interpreted, so one description held in a variable
// reads whatever the source says at the moment it is used -- and a test can
// interpret the same program twice against two sources.
func LoadConfig[R, A any](description config.Config[A]) Effect[R, ConfigError, A] {
	return fromRuntime(func(
		ctx context.Context,
		state *runtimecore.State,
		_ R,
	) Exit[ConfigError, A] {
		value, failure := config.Read(ctx, state.Capabilities().ConfigSource, description)
		if exit, interrupted := interruptedExit[ConfigError, A](ctx); interrupted {
			return exit
		}
		if !failure.IsEmpty() {
			return ExitFailure[ConfigError, A](failure)
		}
		return ExitSuccess[ConfigError](value)
	})
}

// ReadingConfigFrom reads this effect's configuration from a source of its
// own.
//
// The m side of the relation: one runtime, and parts of a program that are
// configured from different places. A plugin reads its settings from the
// document it shipped with while the program around it reads the environment,
// and neither has to know that the other exists.
//
//	plugin.ReadingConfigFrom(config.Beneath(document, "plugins", "billing"))
//
// Runtime-local and inherited, exactly as a span or a name is: work forked
// inside this effect reads from the same source, and the effect after it does
// not.
func (fx Effect[R, E, A]) ReadingConfigFrom(source ConfigSource) Effect[R, E, A] {
	if source == nil {
		return fx
	}
	return fx.withState(func(state *runtimecore.State) *runtimecore.State {
		return state.WithConfigSource(source)
	})
}

// ConfigLayer builds a program's settings as a layer.
//
// Which is what makes settings ordinary. A layer is how this runtime provides
// anything a program requires, so settings provided this way compose with
// ZipLayers beside a database pool and a client, feed the next layer with
// ThenLayers, and are built once however many consumers require them:
//
//	settings := effect.ConfigLayer[Unit](describedSettings)
//	serving := effect.ThenLayers(settings, pool)
//
// The failure stays typed as a ConfigError until somebody adapts it, so a
// composite layer says which half of it could not be built.
func ConfigLayer[RIn, A any](description config.Config[A]) Layer[RIn, ConfigError, A] {
	return LayerFromEffect(LoadConfig[RIn, A](description))
}
