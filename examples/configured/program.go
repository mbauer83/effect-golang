package configured

// Reading the settings, and what makes it ordinary: a layer.

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effect/config"
)

// Refusal is this program's own failure type. It exists to show the adaptation
// every real program needs: settings fail with a ConfigError, and an
// application fails with its own vocabulary.
type Refusal struct {
	Because string
}

// Service is what the program is provided with once its settings have been
// read: two components' settings, built together.
type Service struct {
	Store  Store
	Mailer Mailer
}

type program[A any] = effect.Effect[Service, Refusal, A]

// Settings is the layer that reads both components' descriptions.
//
// One layer, two descriptions, and neither component knows about the other or
// about where the values came from. Both are read whatever the other did, so a
// deployment that has supplied neither is told about both at once instead of
// once per restart.
//
// The failure is adapted here, where the layer is assembled, rather than at
// every use of it.
func Settings() effect.Layer[effect.Unit, Refusal, Service] {
	described := config.ZipWith(
		DescribedStore(),
		DescribedMailer(),
		func(store Store, mailer Mailer) Service {
			return Service{Store: store, Mailer: mailer}
		})
	return effect.ConfigLayer[effect.Unit](described).
		MapError(func(failure effect.ConfigError) Refusal {
			return Refusal{Because: failure.Error()}
		})
}

// Program is the whole thing: settings read into a layer, and a program that
// requires them.
//
// Nothing below here is passed a settings argument, and nothing below here can
// forget one: Service is the requirement channel, so a component that needs a
// setting the layer does not build will not compile.
func Program() effect.Effect[effect.Unit, Refusal, string] {
	return Describe().ProvideLayerSame(Settings()).Named("configured")
}

// Describe is what the program does with its settings, which for an example is
// to say what it was told. The password is in here and cannot leak: Secret
// redacts itself wherever it is formatted, logged or annotated.
func Describe() program[string] {
	operations := effect.For[Service, Refusal]()
	return operations.FromEither(
		func(_ context.Context, service Service) effect.Either[Refusal, string] {
			return effect.Right[Refusal](strings.Join([]string{
				"store " + service.Store.Host +
					" timeout " + service.Store.Timeout.String(),
				"password " + service.Store.Password.String(),
				"limits " + limits(service.Store.Limits),
				"mail from " + service.Mailer.Sender +
					" via " + strings.Join(service.Mailer.Relays, " then ") +
					" retrying " + itoa(service.Mailer.Retries),
			}, "\n"))
		})
}

// ReportingIn reads a plugin's settings from a source the rest of the program
// does not read.
//
// The m side of the relation. The document holds several plugins' settings and
// this one is mounted at its own place inside it, so the plugin's description
// says what it needs and the mounting says where it lives.
func ReportingIn(document config.Source, plugin string) effect.Effect[Service, Refusal, Reporting] {
	return effect.LoadConfig[Service](DescribedReporting()).
		MapError(func(failure effect.ConfigError) Refusal {
			return Refusal{Because: failure.Error()}
		}).
		ReadingConfigFrom(config.Beneath(document, "plugins", plugin)).
		Named("reporting")
}

// StoreFor reads one description twice, under two names.
//
// A dependent read: which store to use is itself configured, and the second
// read depends on the first. That composition is the effect's own FlatMap and
// not something this package repeats -- accumulating composition belongs to a
// description, and sequencing belongs to the runtime.
func StoreFor(which config.Config[string]) effect.Effect[effect.Unit, Refusal, Store] {
	return effect.LoadConfig[effect.Unit](which).
		FlatMap(func(name string) effect.Effect[effect.Unit, effect.ConfigError, Store] {
			return effect.LoadConfig[effect.Unit](config.Nested(name, DescribedStore()))
		}).
		MapError(func(failure effect.ConfigError) Refusal {
			return Refusal{Because: failure.Error()}
		})
}

// Needed is what this program must be told, printed.
//
// The question a description can answer and a function that reads cannot. A
// deployment that has just been told a value is missing can be shown the whole
// list without starting anything.
func Needed() string {
	described := config.ZipWith(
		DescribedStore(),
		DescribedMailer(),
		func(store Store, mailer Mailer) Service {
			return Service{Store: store, Mailer: mailer}
		})
	return config.Document(described.Expects())
}

func limits(held map[string]int) string {
	if len(held) == 0 {
		return "none configured"
	}
	named := make([]string, 0, len(held))
	for name := range held {
		named = append(named, name)
	}
	slices.Sort(named)
	described := make([]string, 0, len(named))
	for _, name := range named {
		described = append(described, name+"="+itoa(held[name]))
	}
	return strings.Join(described, " ")
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
