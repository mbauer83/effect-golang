package config

// The values a source can hold: text, and the types text is read as.
//
// Every one of them is Of with a parser. They are here as named constructors
// because a program says what it wants rather than how to parse it, and
// because the type name each carries is what a printed expectation shows.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Of describes one value read from a source and parsed.
//
// The general primitive, and the seam for a type this package does not
// provide: a log level, a URL, a country code. The type name is what an
// expectation prints, and the parser's error becomes the reason the value was
// refused.
//
// An empty name reads the value at the description's current path rather than
// at a key beneath it. That is what a table's entries and a separated list's
// pieces are, and it is why every named primitive here is Nested applied to a
// nameless one.
func Of[A any](name string, reads string, parse func(string) (A, error)) Config[A] {
	return Config[A]{
		expects: []Expectation{{Path: pathOf(name), Type: reads}},
		read: func(at cursor) (A, Error) {
			var zero A
			here := at.under(name)
			raw, found, err := here.source.Value(here.ctx, here.path)
			switch {
			case err != nil:
				return zero, Unavailable(err, here.path...)
			case !found:
				return zero, Missing(here.path...)
			}
			value, err := parse(raw)
			if err != nil {
				return zero, Invalid(err.Error(), here.path...)
			}
			return value, Error{}
		},
	}
}

// Text reads the value as it is held.
func Text(name string) Config[string] {
	return Of(name, "text", func(raw string) (string, error) {
		return raw, nil
	})
}

// NonEmptyText reads text that is not blank.
//
// Its own primitive because "set to the empty string" is the commonest way a
// deployment supplies nothing while looking like it supplied something: an
// environment variable assigned from an unset variable, a template that
// rendered a missing value, a form field left alone. Text accepts it, and a
// host of "" fails at connect time instead of at start-up.
func NonEmptyText(name string) Config[string] {
	return Of(name, "non-empty text", func(raw string) (string, error) {
		if strings.TrimSpace(raw) == "" {
			return "", errors.New("non-empty text")
		}
		return raw, nil
	})
}

// Int reads a whole number.
func Int(name string) Config[int] {
	return Of(name, "an integer", func(raw string) (int, error) {
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return 0, errors.New("an integer")
		}
		return value, nil
	})
}

// Float reads a number.
func Float(name string) Config[float64] {
	return Of(name, "a number", func(raw string) (float64, error) {
		value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return 0, errors.New("a number")
		}
		return value, nil
	})
}

// Bool reads a flag.
//
// Go's own spellings, plus the ones deployments actually write: on, off, yes
// and no. A spelling this does not know is refused rather than read as false,
// because a flag nobody can tell apart from absence is worse than a failure.
func Bool(name string) Config[bool] {
	return Of(name, "a flag", func(raw string) (bool, error) {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "1", "t", "true", "yes", "y", "on":
			return true, nil
		case "0", "f", "false", "no", "n", "off":
			return false, nil
		default:
			return false, errors.New("a flag: true or false, yes or no, on or off")
		}
	})
}

// Duration reads a length of time in Go's own notation: 250ms, 1m30s.
func Duration(name string) Config[time.Duration] {
	return Of(name, "a duration", func(raw string) (time.Duration, error) {
		value, err := time.ParseDuration(strings.TrimSpace(raw))
		if err != nil {
			return 0, errors.New("a duration, as 250ms or 1m30s")
		}
		return value, nil
	})
}

// Port reads a TCP or UDP port, and refuses one no listener could bind.
//
// A primitive rather than an Int with a range, because every program that
// takes a port wants the same range and the same message, and because a port
// read as an ordinary number fails at bind time instead of at start-up.
func Port(name string) Config[int] {
	return Of(name, "a port", func(raw string) (int, error) {
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || value < 1 || value > 65535 {
			return 0, errors.New("a port between 1 and 65535")
		}
		return value, nil
	})
}

// SecretOf reads a value that must not be printed once it has been read.
//
// A primitive of its own rather than Text a caller remembers to be careful
// with: the value arrives already wrapped, so the type is what keeps it out of
// a log line, and the expectation is marked so a printed one shows the key
// without the value.
func SecretOf(name string) Config[Secret] {
	secret := Of(name, "a secret", func(raw string) (Secret, error) {
		return Secret{value: raw}, nil
	})
	for at := range secret.expects {
		secret.expects[at].Secret = true
	}
	return secret
}

// pathOf is a primitive's own path: one segment, or none when it reads the
// value where it stands.
func pathOf(name string) []string {
	if name == "" {
		return nil
	}
	return []string{name}
}

// renderDefault is how a stand-in appears in a printed expectation. Values a
// program chose itself, so %v is enough and a Secret redacts itself.
func renderDefault[A any](value A) string {
	return fmt.Sprintf("%v", value)
}
