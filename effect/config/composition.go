package config

// Building one description out of several.
//
// Two rules run through all of it. A description that needs both of two things
// reports both when both are wrong, because sending whoever is deploying it
// round the loop once per missing key is the failure mode this exists to
// prevent. And a stand-in applies to absence only: a value that was supplied
// and refused, or a source that could not be reached, is never quietly
// replaced.

import "slices"

// ZipWith reads two descriptions and combines what they produced.
//
// A package function rather than a method because it grows the type it
// produces, and combining rather than pairing because a pair is not what
// anybody wanted: a program's settings are a struct, and the combine function
// is where it is built.
//
//	settings := config.ZipWith(
//	    config.Text("HOST"),
//	    config.Port("PORT"),
//	    func(host string, port int) Address {
//	        return Address{Host: host, Port: port}
//	    })
//
// Both sides are read whatever the first one did, so a deployment missing two
// settings is told about two settings.
func ZipWith[A, B, C any](
	first Config[A],
	second Config[B],
	combine func(A, B) C,
) Config[C] {
	return Config[C]{
		expects: append(slices.Clone(first.expects), second.expects...),
		read: func(at reading) (C, Error) {
			var missing C
			left, leftFailure := first.read(at)
			right, rightFailure := second.read(at)
			if failure := leftFailure.And(rightFailure); !failure.IsEmpty() {
				return missing, failure
			}
			return combine(left, right), Error{}
		},
	}
}

// All reads every description and keeps the values in order.
//
// For the homogeneous case ZipWith is clumsy at: a list of paths that are all
// the same kind of thing. Every one of them is read, so one call reports every
// one that is wrong.
func All[A any](descriptions ...Config[A]) Config[[]A] {
	expects := []Expectation{}
	for _, description := range descriptions {
		expects = append(expects, description.expects...)
	}
	held := slices.Clone(descriptions)
	return Config[[]A]{
		expects: expects,
		read: func(at reading) ([]A, Error) {
			values := make([]A, 0, len(held))
			failure := Error{}
			for _, description := range held {
				value, refused := description.read(at)
				failure = failure.And(refused)
				values = append(values, value)
			}
			if !failure.IsEmpty() {
				return nil, failure
			}
			return values, Error{}
		},
	}
}

// Nested reads a description beneath a name.
//
// What gives a group of settings one place to live, and what lets the same
// description be read twice under two names -- a primary and a replica sharing
// one description of what a database connection needs.
func Nested[A any](name string, of Config[A]) Config[A] {
	held := of.read
	return Config[A]{
		expects: nestedExpectations(name, of.expects),
		read: func(at reading) (A, Error) {
			value, failure := held(at.under(name))
			return value, failure
		},
	}
}

// WithDefault stands in for a value nobody supplied.
//
// Absence only. A value that was supplied and refused is reported, and so is a
// source that could not be consulted -- a default covering either is how a
// typo becomes a program running on a number nobody chose, and how an
// unreachable secret store becomes a service that started without its
// credentials.
func (description Config[A]) WithDefault(value A) Config[A] {
	held := description.read
	stood := slices.Clone(description.expects)
	for at := range stood {
		if stood[at].Default == "" {
			stood[at].Default = renderDefault(value)
		}
	}
	return Config[A]{
		expects: stood,
		read: func(at reading) (A, Error) {
			read, failure := held(at)
			if failure.MissingOnly() {
				return value, Error{}
			}
			return read, failure
		},
	}
}

// OrElse reads an alternative when this description could not be read.
//
// Any failure, not absence alone: an alternative is an explicit statement that
// another description answers the same question, so a value the first one
// refused is a reason to ask the second. Both failures are kept when both
// fail, as alternatives that were all tried.
//
// A source-level fallback is usually what a deployment wants instead -- see
// Sources, which puts the whole description over several sources rather than
// naming two descriptions per value.
func (description Config[A]) OrElse(that Config[A]) Config[A] {
	held := description.read
	other := that.read
	optional := slices.Clone(description.expects)
	for at := range optional {
		optional[at].Optional = true
	}
	return Config[A]{
		expects: append(optional, that.expects...),
		read: func(at reading) (A, Error) {
			value, failure := held(at)
			if failure.IsEmpty() {
				return value, Error{}
			}
			alternative, refused := other(at)
			if refused.IsEmpty() {
				return alternative, Error{}
			}
			var missing A
			return missing, failure.Or(refused)
		},
	}
}

// Optional folds absence into the value's own type.
//
// There is no option type here on purpose. A program that treats a setting as
// optional has to say what its absence means -- no certificate is plaintext, no
// cache is uncached -- and saying it here puts the decision beside the
// description instead of at every use of a value that might not be there.
//
//	security := config.Optional(config.Text("TLS_CERT"),
//	    func(path string) Security { return Security{Certificate: path} },
//	    func() Security { return Security{} })
//
// Absence only, as a default is: a certificate path that was supplied and
// refused is a failure and not a plaintext deployment.
func Optional[A, B any](
	of Config[A],
	supplied func(A) B,
	absent func() B,
) Config[B] {
	held := of.read
	optional := slices.Clone(of.expects)
	for at := range optional {
		optional[at].Optional = true
	}
	return Config[B]{
		expects: optional,
		read: func(at reading) (B, Error) {
			value, failure := held(at)
			switch {
			case failure.MissingOnly():
				return absent(), Error{}
			case !failure.IsEmpty():
				var missing B
				return missing, failure
			}
			return supplied(value), Error{}
		},
	}
}
