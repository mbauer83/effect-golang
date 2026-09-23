package unit

// Where values come from, and what happens when several places could answer.
//
// A different claim from what a description reads: these are about precedence,
// about a source that is down being nothing like a source that does not carry
// a key, and about the spellings a deployment actually uses.

import (
	"context"
	"errors"
	"testing"

	"github.com/mbauer83/effect-golang/effect/config"
)

// unavailable is a source that cannot be consulted, which is the case a
// fallback must not treat as an absence.
type unavailable struct{ err error }

func (source unavailable) Value(context.Context, []string) (string, bool, error) {
	return "", false, source.err
}

func (source unavailable) Children(context.Context, []string) ([]string, error) {
	return nil, source.err
}

func TestTheFirstSourceThatCarriesAPathAnswers(t *testing.T) {
	described := config.ZipWith(
		config.Text("host"),
		config.Int("port"),
		func(host string, port int) address {
			return address{Host: host, Port: port}
		})

	held, failure := config.Read(context.Background(), config.Sources(
		config.FromMap(map[string]string{"port": "9000"}),
		config.FromMap(map[string]string{"host": "from.defaults", "port": "80"}),
	), described)

	if !failure.IsEmpty() {
		t.Fatalf("expected both sources to contribute, got %v", failure)
	}
	// Order is precedence: the override answered for the port, the defaults
	// for the host, and one description read both without knowing.
	if held != (address{Host: "from.defaults", Port: 9000}) {
		t.Fatalf("read %+v", held)
	}
}

func TestASourceThatCannotBeConsultedIsNotAnAbsence(t *testing.T) {
	// The dangerous case. A secret store that is down must not fall through to
	// the defaults beneath it: that starts a program with credentials nobody
	// chose, silently, and only when the store is down.
	down := errors.New("the store refused the connection")
	_, failure := config.Read(context.Background(), config.Sources(
		unavailable{err: down},
		config.FromMap(map[string]string{"token": "from.defaults"}),
	), config.Text("token"))

	if failure.IsEmpty() {
		t.Fatal("expected the unreachable source to be reported")
	}
	leaves := failure.Failures()
	if len(leaves) != 1 || leaves[0].Kind != config.KindUnavailable {
		t.Fatalf("expected one unavailable source, got %v", leaves)
	}
	if !errors.Is(leaves[0].Err, down) {
		t.Fatalf("expected the source's own error, got %v", leaves[0].Err)
	}
	// And a default must not stand in for it either.
	_, defaulted := config.Read(context.Background(), unavailable{err: down},
		config.Text("token").WithDefault("safe"))
	if defaulted.IsEmpty() {
		t.Fatal("expected a default to refuse to stand in for an unreachable source")
	}
}

func TestTheEnvironmentSpellsAPathTheWayDeploymentsWriteIt(t *testing.T) {
	source := config.EnvironmentOf(
		"DB_HOST=primary.internal",
		"DB_MAX_CONNECTIONS=32",
	)
	described := config.Nested("db", config.ZipWith(
		config.Text("host"),
		config.Int("max_connections"),
		func(host string, most int) address {
			return address{Host: host, Port: most}
		}))

	held, failure := config.Read(context.Background(), source, described)
	if !failure.IsEmpty() {
		t.Fatalf("expected the environment to answer, got %v", failure)
	}
	if held != (address{Host: "primary.internal", Port: 32}) {
		t.Fatalf("read %+v", held)
	}
}

func TestOneDescriptionReadsTwoSpellingsThroughRenaming(t *testing.T) {
	// A description written in one vocabulary, read from a source that spells
	// it another way, without saying it twice.
	described := config.Nested("db", config.Text("maxConnections"))
	source := config.MapInput(
		config.FromMap(map[string]string{"db.max-connections": "16"}),
		kebab)

	value, failure := config.Read(context.Background(), source, described)
	if !failure.IsEmpty() || value != "16" {
		t.Fatalf("expected the renamed path to answer, got %q and %v", value, failure)
	}
}

func kebab(segment string) string {
	spelled := []rune{}
	for _, letter := range segment {
		if letter >= 'A' && letter <= 'Z' {
			spelled = append(spelled, '-', letter+('a'-'A'))
			continue
		}
		spelled = append(spelled, letter)
	}
	return string(spelled)
}

func TestADescriptionCanBeMountedInsideADocument(t *testing.T) {
	// The same description, twice, at two places in one source: what makes a
	// component's settings reusable rather than a section of one program's
	// file.
	document := config.FromMap(map[string]string{
		"services.billing.host": "billing.internal",
		"services.search.host":  "search.internal",
		"services.search.port":  "9200",
	})
	described := addressDescription()

	billing, failure := config.Read(context.Background(),
		config.Beneath(document, "services", "billing"), described)
	if !failure.IsEmpty() {
		t.Fatalf("expected billing to read, got %v", failure)
	}
	search, failure := config.Read(context.Background(),
		config.Beneath(document, "services", "search"), described)
	if !failure.IsEmpty() {
		t.Fatalf("expected search to read, got %v", failure)
	}
	if billing.Host != "billing.internal" || billing.Port != 8080 {
		t.Fatalf("billing read as %+v", billing)
	}
	if search.Host != "search.internal" || search.Port != 9200 {
		t.Fatalf("search read as %+v", search)
	}
}

func TestATableReadsOneEntryPerKeyTheSourceHolds(t *testing.T) {
	source := config.EnvironmentOf("LIMITS_READ=100", "LIMITS_WRITE=25")

	limits, failure := config.Read(context.Background(), source,
		config.Table("limits", config.Int("")))
	if !failure.IsEmpty() {
		t.Fatalf("expected the table to read, got %v", failure)
	}
	if len(limits) != 2 || limits["read"] != 100 || limits["write"] != 25 {
		t.Fatalf("read %v", limits)
	}

	// Nothing beneath the name is an empty table and not a failure: a program
	// with no limits configured has none.
	empty, failure := config.Read(context.Background(), config.FromMap(nil),
		config.Table("limits", config.Int("")))
	if !failure.IsEmpty() || len(empty) != 0 {
		t.Fatalf("expected an empty table, got %v and %v", empty, failure)
	}
}

func TestATableOfGroupsReadsAFieldOfEachEntry(t *testing.T) {
	source := config.EnvironmentOf(
		"QUEUES_JOBS_DEPTH=64", "QUEUES_JOBS_WORKERS=4",
		"QUEUES_MAIL_DEPTH=8", "QUEUES_MAIL_WORKERS=1",
	)
	described := config.Table("queues", config.ZipWith(
		config.Int("depth"), config.Int("workers"),
		func(depth int, workers int) address {
			return address{Port: depth, Host: string(rune('0' + workers))}
		}))

	queues, failure := config.Read(context.Background(), source, described)
	if !failure.IsEmpty() {
		t.Fatalf("expected both queues to read, got %v", failure)
	}
	if queues["jobs"].Port != 64 || queues["mail"].Port != 8 {
		t.Fatalf("read %v", queues)
	}
}

func TestSeveralValuesHeldInOneKeyAreReadByTheEntryDescription(t *testing.T) {
	source := config.FromMap(map[string]string{"ports": "8080, 8081,8082"})

	ports, failure := config.Read(context.Background(), source,
		config.Many("ports", ",", config.Port("")))
	if !failure.IsEmpty() {
		t.Fatalf("expected the list to read, got %v", failure)
	}
	if len(ports) != 3 || ports[0] != 8080 || ports[2] != 8082 {
		t.Fatalf("read %v", ports)
	}

	// A piece the entry description refuses is refused, and says so against
	// the key it was in rather than against a position nobody can see.
	_, refused := config.Read(context.Background(),
		config.FromMap(map[string]string{"ports": "8080,0"}),
		config.Many("ports", ",", config.Port("")))
	leaves := refused.Failures()
	if len(leaves) != 1 || config.Render(leaves[0].Path) != "ports" {
		t.Fatalf("expected one refusal against ports, got %v", leaves)
	}
}
