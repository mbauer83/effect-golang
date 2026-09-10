package unit

// Reading the requirements an effect was given.
//
// A Layer built an environment and Provide satisfied one, and nothing could
// ask for it: the only way to reach R was a leaf whose function takes it as an
// argument. That is enough for an adapter and not enough for an application,
// which is why a program that meant to keep its dependencies in R ended up
// threading them as arguments and leaving R unused.

import (
	"context"
	"errors"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
)

// clockAndGreeting is a small environment: two things a program was given.
type clockAndGreeting struct {
	Greeting string
	Shout    bool
}

func TestAComposedFunctionCanAskForItsRequirements(t *testing.T) {
	// The property that makes R usable for dependencies: a function written
	// against its requirements, composed like anything else, without
	// bottoming out in a leaf that takes them as an argument.
	operations := effect.For[clockAndGreeting, error]()

	greeting := operations.Environment().
		FlatMap(func(given clockAndGreeting) effect.Effect[clockAndGreeting, error, string] {
			if given.Shout {
				return operations.Succeed(given.Greeting + "!")
			}
			return operations.Succeed(given.Greeting)
		})

	value, ok := effect.Run(context.Background(),
		clockAndGreeting{Greeting: "hello", Shout: true}, greeting).Value()
	if !ok || value != "hello!" {
		t.Fatalf("expected the environment read, got %q (ok=%v)", value, ok)
	}
}

func TestAskingForOneRequirementNamesItWhereItIsUsed(t *testing.T) {
	// So a reader sees what a function depends on without reading its body.
	asked := effect.EnvironmentWith[clockAndGreeting, error](
		func(given clockAndGreeting) string { return given.Greeting })

	value, ok := effect.Run(context.Background(),
		clockAndGreeting{Greeting: "good evening"}, asked).Value()
	if !ok || value != "good evening" {
		t.Fatalf("expected the one member read, got %q (ok=%v)", value, ok)
	}
}

func TestALayerBuildsWhatTheProgramThenAsksFor(t *testing.T) {
	// The whole arrangement: a layer constructs the environment effectfully,
	// the program asks for it, and neither knows how the other is written.
	built := effect.LayerFromEffect(effect.For[effect.Unit, error]().
		Try(func(context.Context, effect.Unit) (clockAndGreeting, error) {
			return clockAndGreeting{Greeting: "from a layer", Shout: false}, nil
		}, func(err error) error { return err }))

	program := effect.Environment[clockAndGreeting, error]().
		Map(func(given clockAndGreeting) string { return given.Greeting })

	value, ok := effect.Run(context.Background(), effect.Unit{},
		program.ProvideLayerSame(built)).Value()
	if !ok || value != "from a layer" {
		t.Fatalf("expected the layer's environment, got %q (ok=%v)", value, ok)
	}
}

func TestALayerThatCannotBuildFailsTheProgramItWasFor(t *testing.T) {
	// And a dependency that could not be constructed is a failure of the
	// program that needed it rather than a nil somebody dereferences later.
	refused := errors.New("the greeting could not be read")
	failing := effect.LayerFromEffect(effect.Fail[effect.Unit, clockAndGreeting](refused))

	program := effect.Environment[clockAndGreeting, error]().
		Map(func(given clockAndGreeting) string { return given.Greeting })

	cause, failed := effect.Run(context.Background(), effect.Unit{},
		program.ProvideLayerSame(failing)).Cause()
	if !failed {
		t.Fatal("expected the program to fail with its layer")
	}
	if failures := cause.Failures(); len(failures) != 1 || !errors.Is(failures[0], refused) {
		t.Fatalf("expected the layer's own refusal, got %s", cause.String())
	}
}
