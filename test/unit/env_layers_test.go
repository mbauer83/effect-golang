package unit

// Composing a program's dependencies group by group.

import (
	"errors"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effect/env"
)

// A composition root builds groups, not dependencies, and some groups need an
// earlier group's work before they exist. These are the two things that has to
// be true of composing them: a later group can read what an earlier one
// provided, and the environment that comes out holds every group's work rather
// than the last one's.

type opened struct{ named string }
type films struct{ from string }
type kept struct{ size int }

func TestALaterGroupReadsWhatAnEarlierOneProvided(t *testing.T) {
	database := env.Adding[error](effect.Succeed[env.Services, error](
		env.Empty().With(opened{named: "films.db"})))
	records := env.Adding[error](env.Needing[error]().Service[opened]().
		Map(func(open opened) env.Services {
			return env.Empty().With(films{from: open.named})
		}))

	assembled := ranToCompletion(t, env.Assembled(database, records).Build().Provide(env.Empty()))

	found, present := env.Resolve[films](assembled)
	if !present {
		t.Fatal("the group that read the database provided nothing")
	}
	if found.from != "films.db" {
		t.Fatalf("the later group did not see the earlier one's work: %+v", found)
	}
}

func TestAssemblingKeepsEveryGroupsWork(t *testing.T) {
	assembled := ranToCompletion(t, env.Assembled(
		env.Held[error](env.Empty().With(opened{named: "films.db"})),
		env.Held[error](env.Empty().With(films{from: "somewhere"})),
		env.Held[error](env.Empty().With(kept{size: 500})),
	).Build().Provide(env.Empty()))

	for _, held := range []bool{
		env.Holds[opened](assembled), env.Holds[films](assembled), env.Holds[kept](assembled),
	} {
		if !held {
			t.Fatalf("assembling three groups kept %v", assembled.Named())
		}
	}
}

func TestTheLaterGroupWinsACollision(t *testing.T) {
	assembled := ranToCompletion(t, env.Assembled(
		env.Held[error](env.Empty().With(kept{size: 1})),
		env.Held[error](env.Empty().With(kept{size: 500})),
	).Build().Provide(env.Empty()))

	found, _ := env.Resolve[kept](assembled)
	if found.size != 500 {
		t.Fatalf("the earlier group won: %+v", found)
	}
}

// An effect that requires nothing runs where something is required, which is
// what lets a domain declare no requirements and still compose with the
// application step that has ten.
func TestSomethingRequiringNothingRunsWhereSomethingIsRequired(t *testing.T) {
	requiring := effect.Anywhere[env.Services, error](
		effect.Succeed[effect.Unit, error](7))
	if got := ranToCompletion(t, requiring.Provide(env.Empty())); got != 7 {
		t.Fatalf("widened effect answered %v", got)
	}
}

// ranToCompletion runs an effect that requires nothing and hands back its
// value, failing the test on any other exit.
func ranToCompletion[A any](t *testing.T, fx effect.Effect[effect.Unit, error, A]) A {
	t.Helper()
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	exit := runtime.Run(t.Context(), effect.Unit{}, fx)
	value, succeeded := exit.Value()
	if !succeeded {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	return value
}

// Several parts of a program asking for the same dependency is the normal
// case, and a report that named it once per asker would read as several
// problems.
func TestOneAbsenceIsReportedOnce(t *testing.T) {
	err := env.Complete(env.Empty(),
		env.Want[films](), env.Want[films](), env.Want[films](), env.Want[kept]())

	var absent env.Incomplete
	if !errors.As(err, &absent) {
		t.Fatalf("not an incompleteness: %T %v", err, err)
	}
	if len(absent.Absent) != 2 {
		t.Fatalf("two absences named %d times: %v", len(absent.Absent), absent.Absent)
	}
}
