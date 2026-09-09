package config

// Describing a settings type one field at a time.
//
// ZipWith combines two descriptions, which is what a pair of values wants and
// what a struct of five does not: each field arrives as another nesting and
// another closure whose whole job is to assign one field, so the shape of the
// code stops resembling the shape of the settings. Struct is the same
// accumulation written flat, and it is the form schema already uses for the
// same reason.

import "slices"

// Field is one field of a described settings type.
//
// The value's own type is captured inside the closure rather than appearing in
// this type, which is what lets fields of different types sit in one list
// without a top type anywhere.
type Field[S any] struct {
	read    func(at reading, into *S) Error
	expects []Expectation
}

// Setting describes one field: where its value comes from, and where it goes.
//
//	config.Setting(config.Port("port").WithDefault(5432),
//	    func(store *Store, port int) { store.Port = port })
//
// The description carries the field's name, its type, its default and its
// documentation, so the assignment is the only thing left to say.
func Setting[S, A any](of Config[A], assign func(*S, A)) Field[S] {
	read := of.reader()
	return Field[S]{
		expects: of.expects,
		read: func(at reading, into *S) Error {
			value, failure := read(at)
			if !failure.IsEmpty() {
				return failure
			}
			assign(into, value)
			return Error{}
		},
	}
}

// Struct describes a settings type from its fields.
//
//	func DescribedStore() config.Config[Store] {
//	    return config.Nested("db", config.Struct(
//	        config.Setting(config.NonEmptyText("host"),
//	            func(store *Store, host string) { store.Host = host }),
//	        config.Setting(config.Port("port").WithDefault(5432),
//	            func(store *Store, port int) { store.Port = port }),
//	    ))
//	}
//
// Every field is read whatever the ones before it did, and the failures
// accumulate exactly as ZipWith's do -- which is the property this package
// exists for, and the reason this is not a loop over reflected struct tags: a
// deployment that has supplied none of five settings is told about five.
//
// The value is built into a fresh S and returned only if every field
// succeeded, so a half-assembled settings type never escapes.
func Struct[S any](fields ...Field[S]) Config[S] {
	heldValue := slices.Clone(fields)
	expects := []Expectation{}
	for _, field := range heldValue {
		expects = append(expects, field.expects...)
	}
	return Config[S]{
		expects: expects,
		read: func(at reading) (S, Error) {
			var built S
			failure := Error{}
			for _, field := range heldValue {
				if field.read == nil {
					failure = failure.And(errZeroDescription)
					continue
				}
				failure = failure.And(field.read(at, &built))
			}
			if !failure.IsEmpty() {
				var missing S
				return missing, failure
			}
			return built, Error{}
		},
	}
}
