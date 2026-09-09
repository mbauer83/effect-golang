package unit

// Describing a settings type field by field.
//
// A different claim from what one value reads: this is about the flat form of
// the accumulation -- every field read whatever the ones before it did, no
// half-assembled value escaping, and a description nobody built reported
// rather than dereferenced.

import (
	"strings"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang/effect/config"
)

func TestEveryFieldOfAStructIsReadAndItsFailuresAccumulate(t *testing.T) {
	// The flat form of the same accumulation ZipWith does. A settings type of
	// five fields written as four nested ZipWiths stops resembling the
	// settings; what must not change is that all five are read.
	described := config.Struct(
		config.Setting(config.NonEmptyText("host"),
			func(held *address, host string) { held.Host = host }),
		config.Setting(config.Port("port").WithDefault(8080),
			func(held *address, port int) { held.Port = port }),
		config.Setting(config.Duration("timeout"),
			func(held *address, timeout time.Duration) { held.Timeout = timeout }),
	)

	held, failure := read(t, map[string]string{
		"host":    "0.0.0.0",
		"timeout": "1s",
	}, described)
	if !failure.IsEmpty() {
		t.Fatalf("expected the fields to read, got %v", failure)
	}
	if held != (address{Host: "0.0.0.0", Port: 8080, Timeout: time.Second}) {
		t.Fatalf("read %+v", held)
	}

	// Two fields wrong, two failures, in the order the fields were written.
	_, refused := read(t, map[string]string{"port": "0"}, described)
	leaves := refused.Failures()
	if len(leaves) != 3 {
		t.Fatalf("expected every field reported, got %v", leaves)
	}
	for at, want := range []string{"host", "port", "timeout"} {
		if got := config.Render(leaves[at].Path); got != want {
			t.Fatalf("expected %q at %d, got %q", want, at, got)
		}
	}
	// A field that succeeded does not escape beside the ones that failed.
	if value, _ := read(t, map[string]string{"port": "9000"}, described); value.Port != 0 {
		t.Fatalf("a half-assembled value escaped: %+v", value)
	}
	// And the expectations are in field order, which is what Document prints.
	expects := described.Expects()
	if len(expects) != 3 || config.Render(expects[0].Path) != "host" {
		t.Fatalf("expected the fields in order, got %v", expects)
	}
}

func TestADescriptionNobodyBuiltIsReportedRatherThanPanicking(t *testing.T) {
	// A declared and unassigned Config is a mistake in the program, and it is
	// reported beside whatever else was wrong rather than as a nil call at the
	// bottom of a stack.
	var forgotten config.Config[string]
	described := config.Struct(
		config.Setting(config.Text("host"),
			func(held *address, host string) { held.Host = host }),
		config.Setting(forgotten,
			func(held *address, spelled string) { held.Host = spelled }),
	)

	_, failure := read(t, map[string]string{"host": "here"}, described)
	if failure.IsEmpty() {
		t.Fatal("expected the zero description to be reported")
	}
	if failure.MissingOnly() {
		t.Fatalf("expected a programming mistake rather than an absence: %v", failure)
	}
	// Nested and mapped, it is still reported rather than dereferenced.
	if _, refused := read(t, nil, config.Nested("db", forgotten)); refused.IsEmpty() {
		t.Fatal("expected a nested zero description to be reported")
	}
	if _, refused := read(t, nil, forgotten.Map(strings.ToUpper)); refused.IsEmpty() {
		t.Fatal("expected a mapped zero description to be reported")
	}
}
