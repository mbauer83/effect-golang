package config

import "log/slog"

// Secret is a value that must not appear in output.
//
// A type rather than a convention, because the leak it prevents is not a
// mistake anybody makes deliberately: a token read as text ends up in a log
// line, a span annotation or an error message the first time somebody adds one,
// and nothing in the code said it should not.
//
// It is opaque three ways, which are the three ways a Go value leaves a
// program: String for anything formatted, MarshalText for anything encoded,
// and LogValue for slog -- which is what this runtime's logs, annotations and
// events are made of. Reveal is the only way out, and it is a word a reviewer
// can search for.
type Secret struct {
	value string
}

// NewSecret wraps a value a program already has, so a secret from somewhere
// other than a config source travels the same way.
func NewSecret(value string) Secret {
	return Secret{value: value}
}

// Reveal returns the value.
//
// Every use is a decision to let it out: hand it to the client that needs it,
// and never to a formatter, a logger or an error.
func (secret Secret) Reveal() string {
	return secret.value
}

// IsEmpty reports whether there is nothing here, which a program may need to
// know without looking.
func (secret Secret) IsEmpty() bool {
	return secret.value == ""
}

// String is what fmt prints.
func (Secret) String() string {
	return redacted
}

// MarshalText is what encoding/json and every other text encoder writes.
//
// Redacted rather than refused: a settings struct that could not be encoded at
// all would be one nobody could dump while debugging, and the point is that
// dumping it is safe.
func (Secret) MarshalText() ([]byte, error) {
	return []byte(redacted), nil
}

// LogValue is what slog records, so an attribute carrying a secret redacts
// itself wherever this runtime's logs, annotations and events are read.
func (Secret) LogValue() slog.Value {
	return slog.StringValue(redacted)
}

const redacted = "<redacted>"
