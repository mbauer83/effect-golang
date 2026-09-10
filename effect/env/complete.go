package env

// Whether a program was given everything it will ask for.
//
// The answer to what a type-indexed environment gives up. Under one interface
// per dependency, a step asking for something the wiring does not hold is a
// compile error; here it is a defect at first use, which for a rarely-taken
// path could be a defect in production on a Tuesday.
//
// So a program says what it needs and the wiring is checked against it before
// anything serves. That is a list stated in one place, which is a cost -- but
// it is one list for a program rather than one interface and one accessor per
// dependency, and it is checked rather than trusted.

import (
	"reflect"
	"strings"
)

// Wanted is the type of one dependency a program will ask for.
type Wanted struct {
	named reflect.Type
}

// Want names one dependency a program will ask for.
//
//	env.Want[catalog.Repository]()
func Want[A any]() Wanted {
	return Wanted{named: reflect.TypeFor[A]()}
}

// String is the type's own name, for a report.
func (wanted Wanted) String() string {
	if wanted.named == nil {
		return "an unnamed dependency"
	}
	return wanted.named.String()
}

// Complete reports which of these a program was not given, in the order they
// were named.
//
// Every one of them rather than the first, because a deployment missing three
// dependencies wants to be told three times: reporting one sends whoever is
// wiring it round the loop three times.
//
// Once each, though. Several rings asking for the same dependency is the
// normal case -- four of them want a shelf -- and a report naming it four
// times reads as four problems.
func Complete(services Services, wanted ...Wanted) error {
	absent := []string{}
	named := map[reflect.Type]bool{}
	for _, want := range wanted {
		if want.named == nil || named[want.named] {
			continue
		}
		named[want.named] = true
		if _, present := services.held[want.named]; !present {
			absent = append(absent, want.String())
		}
	}
	if len(absent) == 0 {
		return nil
	}
	return Incomplete{Absent: absent, Given: services.Named()}
}

// Incomplete is a program that will ask for dependencies it was not given.
type Incomplete struct {
	Absent []string
	Given  []string
}

func (incomplete Incomplete) Error() string {
	return "env: this program will ask for dependencies it was not given: " +
		strings.Join(incomplete.Absent, ", ") + "; it was given " +
		strings.Join(incomplete.Given, ", ")
}
