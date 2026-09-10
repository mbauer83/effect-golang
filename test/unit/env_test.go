package unit

// A program's dependencies, each found by its own type.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effect/env"
	"github.com/mbauer83/effect-golang/experimental/direct"
)

// Two ports, as an application would have them.
type Films interface{ Title(int) string }
type Clock interface{ Year() int }

type keptFilms struct{}

func (keptFilms) Title(int) string { return "Heat" }

type wallClock struct{}

func (wallClock) Year() int { return 2026 }

func TestADependencyIsFoundByItsOwnType(t *testing.T) {
	// Declaring one is providing it and asking for one is naming it, with no
	// interface and no accessor per dependency in between.
	given := env.Empty().
		With[Films](keptFilms{}).
		With[Clock](wallClock{})

	films, held := env.Resolve[Films](given)
	if !held || films.Title(949) != "Heat" {
		t.Fatalf("expected the film repository, got %v (held=%v)", films, held)
	}
	if _, held := env.Resolve[Clock](given); !held {
		t.Fatal("expected the clock as well, since one type does not shadow another")
	}
}

func TestTheDeclaredTypeIsWhatAStepAsksFor(t *testing.T) {
	// A dependency registered as its concrete type is not the one a step
	// asking for the interface finds -- so the caller says which it means.
	concrete := env.Empty().With(keptFilms{})

	if env.Holds[Films](concrete) {
		t.Fatal("expected a concrete registration not to answer for the interface")
	}
	if !env.Holds[keptFilms](concrete) {
		t.Fatal("expected it to answer for what it was registered as")
	}
}

func TestAnEnvironmentDoesNotChangeUnderneathWhatWasGivenIt(t *testing.T) {
	// So a layer composing two cannot change either, and an environment a
	// fiber holds stays what it was.
	first := env.Empty().With[Films](keptFilms{})
	second := first.With[Clock](wallClock{})

	if env.Holds[Clock](first) {
		t.Fatal("expected the first environment untouched")
	}
	if !env.Holds[Clock](second) {
		t.Fatal("expected the second to hold both")
	}
}

func TestAStepResolvesWhatItNeedsAndReadsAsOrdinaryGo(t *testing.T) {
	needs := env.Needing[error]()
	given := env.Empty().With[Films](keptFilms{}).With[Clock](wallClock{})

	program := direct.Run(func(bind *direct.Binder[env.Services, error]) string {
		films := direct.Bind(bind, needs.Service[Films]())
		now := direct.Bind(bind, needs.Service[Clock]())
		return films.Title(949) + ", read in " + itoa(now.Year())
	})

	value, ok := effect.Run(context.Background(), given, program).Value()
	if !ok || value != "Heat, read in 2026" {
		t.Fatalf("expected the two dependencies resolved, got %q (ok=%v)", value, ok)
	}
}

func TestADependencyNobodyProvidedIsADefectAndNotAFailure(t *testing.T) {
	// A missing dependency is a mis-wired program: no caller can act on it,
	// no retry helps, and putting it in the failure channel would make every
	// caller handle a case that means the deployment is broken.
	needs := env.Needing[error]()
	given := env.Empty().With[Films](keptFilms{})

	program := direct.Run(func(bind *direct.Binder[env.Services, error]) int {
		return direct.Bind(bind, needs.Service[Clock]()).Year()
	})

	cause, failed := effect.Run(context.Background(), given, program).Cause()
	if !failed {
		t.Fatal("expected the missing dependency to stop the program")
	}
	if !cause.ContainsDefect() {
		t.Fatalf("expected a defect rather than a failure, got %s", cause.String())
	}
	// And it says what was wanted and what the program was given, because
	// that is the whole of what somebody fixing the wiring needs.
	rendered := cause.String()
	for _, wanted := range []string{"Clock", "Films"} {
		if !strings.Contains(rendered, wanted) {
			t.Errorf("expected %q named in the defect, got %s", wanted, rendered)
		}
	}
}

func TestAProgramIsToldAtStartUpWhatItWasNotGiven(t *testing.T) {
	// Which is the answer to what a type-indexed environment gives up: the
	// compiler no longer catches it, so the wiring is checked before anything
	// serves rather than on a Tuesday down a rarely-taken path.
	given := env.Empty().With[Films](keptFilms{})

	err := env.Complete(given, env.Want[Films](), env.Want[Clock]())
	if err == nil {
		t.Fatal("expected the absent dependency reported")
	}
	if !strings.Contains(err.Error(), "Clock") {
		t.Fatalf("expected the absent one named, got %v", err)
	}
	if strings.Contains(err.Error(), "will ask for dependencies it was not given: unit.Films") {
		t.Fatalf("expected the one it has not reported as absent, got %v", err)
	}

	// Every absent one, because a deployment missing three wants telling
	// three times rather than going round the loop three times.
	err = env.Complete(env.Empty(), env.Want[Films](), env.Want[Clock]())
	var incomplete env.Incomplete
	if !errors.As(err, &incomplete) || len(incomplete.Absent) != 2 {
		t.Fatalf("expected both reported, got %v", err)
	}

	if err := env.Complete(given, env.Want[Films]()); err != nil {
		t.Fatalf("expected a complete environment to pass, got %v", err)
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := [20]byte{}
	at := len(digits)
	for value > 0 {
		at--
		digits[at] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[at:])
}
