package unit

// What a description reads, and what it refuses.
//
// The claims here are the ones a program's settings depend on being exactly
// right: that a default stands in for absence and for nothing else, that a
// composite reports every failure rather than the first, and that a value
// read as the wrong type is a failure rather than a zero.

import (
	"context"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang/effect/config"
)

type address struct {
	Host    string
	Port    int
	Timeout time.Duration
}

// addressDescription is one description used by several tests, because that is
// how a program uses one: written once, read against whatever source the
// deployment or the test has.
func addressDescription() config.Config[address] {
	return config.ZipWith(
		config.ZipWith(
			config.NonEmptyText("host").WithDescription("the address to listen on"),
			config.Port("port").WithDefault(8080),
			func(host string, port int) address {
				return address{Host: host, Port: port}
			}),
		config.Duration("timeout").WithDefault(5*time.Second),
		func(held address, timeout time.Duration) address {
			held.Timeout = timeout
			return held
		})
}

func read[A any](t *testing.T, values map[string]string, description config.Config[A]) (A, config.Error) {
	t.Helper()
	return config.Read(context.Background(), config.FromMap(values), description)
}

func TestADescriptionReadsWhatTheSourceHolds(t *testing.T) {
	held, failure := read(t, map[string]string{
		"host":    "0.0.0.0",
		"port":    "9000",
		"timeout": "250ms",
	}, addressDescription())

	if !failure.IsEmpty() {
		t.Fatalf("expected the settings to read, got %v", failure)
	}
	if held != (address{Host: "0.0.0.0", Port: 9000, Timeout: 250 * time.Millisecond}) {
		t.Fatalf("read %+v", held)
	}
}

func TestADefaultStandsInForAbsenceOnly(t *testing.T) {
	// The claim this whole package exists to get right. A value nobody
	// supplied is what a default is for; a value that was supplied and cannot
	// be read is a mistake, and standing in for it would start the program on
	// a number nobody chose and say nothing.
	held, failure := read(t, map[string]string{"host": "localhost"}, addressDescription())
	if !failure.IsEmpty() {
		t.Fatalf("expected the defaults to stand in, got %v", failure)
	}
	if held.Port != 8080 || held.Timeout != 5*time.Second {
		t.Fatalf("expected the defaults, read %+v", held)
	}

	_, refused := read(t, map[string]string{
		"host": "localhost",
		"port": "eighty-eighty",
	}, addressDescription())
	if refused.IsEmpty() {
		t.Fatal("expected a port that is not a number to be refused")
	}
	leaves := refused.Failures()
	if len(leaves) != 1 || leaves[0].Kind != config.KindInvalid {
		t.Fatalf("expected one invalid value, got %v", leaves)
	}
	if got := config.Render(leaves[0].Path); got != "port" {
		t.Fatalf("expected the failure against port, got %q", got)
	}
}

func TestEveryMissingSettingIsReportedAtOnce(t *testing.T) {
	// A deployment told about one missing key at a time is a deployment
	// restarted once per key.
	described := config.ZipWith(
		config.Text("host"),
		config.All(config.Int("first"), config.Int("second")),
		func(host string, ports []int) address {
			return address{Host: host, Port: ports[0]}
		})

	_, failure := read(t, map[string]string{}, described)
	leaves := failure.Failures()
	if len(leaves) != 3 {
		t.Fatalf("expected all three reported, got %d: %v", len(leaves), failure)
	}
	if !failure.MissingOnly() {
		t.Fatalf("expected every failure to be an absence, got %v", failure)
	}
	// In the order the description asks, which is the order somebody reading
	// the output expects.
	for at, want := range []string{"host", "first", "second"} {
		if got := config.Render(leaves[at].Path); got != want {
			t.Fatalf("expected %q at %d, got %q", want, at, got)
		}
	}
	if failure.Error() == "" {
		t.Fatal("expected the composite to render")
	}
}

func TestABlankValueIsNotAValue(t *testing.T) {
	// Present and empty is a deployment that meant to supply something: an
	// environment variable set to "" is the classic one.
	_, failure := read(t, map[string]string{"host": "  "}, config.NonEmptyText("host"))
	if failure.IsEmpty() {
		t.Fatal("expected blank text to be refused")
	}
	if failure.MissingOnly() {
		t.Fatal("expected a refusal rather than an absence: it was supplied")
	}
}

func TestNestedDescriptionsReadBeneathTheirName(t *testing.T) {
	described := config.ZipWith(
		config.Nested("primary", addressDescription()),
		config.Nested("replica", addressDescription()),
		func(primary address, replica address) []address {
			return []address{primary, replica}
		})

	both, failure := read(t, map[string]string{
		"primary.host": "one",
		"replica.host": "two",
		"replica.port": "9001",
	}, described)
	if !failure.IsEmpty() {
		t.Fatalf("expected both to read, got %v", failure)
	}
	if both[0].Host != "one" || both[0].Port != 8080 {
		t.Fatalf("primary read as %+v", both[0])
	}
	if both[1].Host != "two" || both[1].Port != 9001 {
		t.Fatalf("replica read as %+v", both[1])
	}

	// And a failure says which of the two it was about.
	_, refused := read(t, map[string]string{"primary.host": "one"}, described)
	leaves := refused.Failures()
	if len(leaves) != 1 || config.Render(leaves[0].Path) != "replica.host" {
		t.Fatalf("expected the failure under replica, got %v", leaves)
	}
}

func TestAnAlternativeIsTriedAndBothFailuresAreKept(t *testing.T) {
	described := config.Int("new").OrElse(config.Int("old"))

	value, failure := read(t, map[string]string{"old": "3"}, described)
	if !failure.IsEmpty() || value != 3 {
		t.Fatalf("expected the alternative to answer, got %d and %v", value, failure)
	}

	_, refused := read(t, map[string]string{}, described)
	if refused.Kind() != config.KindOr {
		t.Fatalf("expected alternatives that were all tried, got %v", refused.Kind())
	}
	if len(refused.Failures()) != 2 {
		t.Fatalf("expected both alternatives reported, got %v", refused)
	}
}

func TestAbsenceIsFoldedIntoTheValuesOwnType(t *testing.T) {
	type security struct{ Certificate string }
	described := config.Optional(config.NonEmptyText("cert"),
		func(path string) security { return security{Certificate: path} },
		func() security { return security{} })

	plain, failure := read(t, map[string]string{}, described)
	if !failure.IsEmpty() || plain != (security{}) {
		t.Fatalf("expected absence folded, got %+v and %v", plain, failure)
	}

	// A certificate that was supplied and refused is a failure and not a
	// plaintext deployment, which is the whole difference between this and a
	// nullable field.
	_, refused := read(t, map[string]string{"cert": ""}, described)
	if refused.IsEmpty() {
		t.Fatal("expected a supplied and unusable value to be refused")
	}
}

func TestAValidatedValueSaysWhatWasWanted(t *testing.T) {
	described := config.Int("workers").
		Validate("at least one worker", func(count int) bool { return count >= 1 })

	if _, failure := read(t, map[string]string{"workers": "4"}, described); !failure.IsEmpty() {
		t.Fatalf("expected four workers to pass, got %v", failure)
	}
	_, refused := read(t, map[string]string{"workers": "0"}, described)
	leaves := refused.Failures()
	if len(leaves) != 1 || leaves[0].Message != "at least one worker" {
		t.Fatalf("expected the message beside the value, got %v", leaves)
	}
	if config.Render(leaves[0].Path) != "workers" {
		t.Fatalf("expected the refusal against workers, got %v", leaves[0].Path)
	}
}
