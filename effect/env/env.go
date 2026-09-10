// Package env is a program's dependencies, each found by its own type.
//
// The alternative it replaces was one interface and one accessor per
// dependency: an environment declaring "I can hand over a film repository"
// as a method, and a step naming that interface in its own constraint. That
// is checked at compile time, which is worth something, and it costs a
// hundred and thirty lines of boilerplate for ten dependencies -- and the
// boilerplate is the same boilerplate every time.
//
// It cannot be made generic. A single Has[A] interface would need one method
// name for every A, and Go has no method overloading: MEASURED, a type
// answering both Has[Films] and Has[Clock] does not compile, "method
// twoThings.Get already declared". So the choice is a distinct method per
// dependency or no method at all.
//
// This is no method at all. A dependency is found by its own type, so
// declaring one is providing it and asking for one is naming it:
//
//	given := env.Empty().With(films).With(upstream)
//	repository := env.Resolve[catalog.Repository](given)
//
// What that trades is when the mistake is found. A step asking for something
// the wiring does not hold is a compile error under the interfaces and a
// defect at first use here -- and a defect rather than a failure because a
// missing dependency is a mis-wired program, not an outcome a caller can act
// on. Complete answers that question at start-up, which is where a wiring
// mistake belongs, so what is lost is the compiler and not the earliness.
package env

import (
	"reflect"
	"sort"
	"strings"
)

// Services is a program's dependencies.
//
// Immutable: With answers a new one, so a layer composing two environments
// cannot change either, and an environment handed to a fiber stays what it
// was.
type Services struct {
	held map[reflect.Type]any
}

// Empty is an environment holding nothing.
func Empty() Services {
	return Services{held: map[reflect.Type]any{}}
}

// With is this environment and one more dependency, found by its own type.
//
// The type is the declared one and not the concrete one: With[catalog.Repository]
// on a *store.Films registers the interface, which is what a step asks for.
// Inference takes the concrete type, so a caller that means the interface says
// so -- given.With[catalog.Repository](films) -- and one that does not gets a
// dependency nobody asks for, which Complete reports.
func (services Services) With[A any](service A) Services {
	held := make(map[reflect.Type]any, len(services.held)+1)
	for named, existing := range services.held {
		held[named] = existing
	}
	held[reflect.TypeFor[A]()] = service
	return Services{held: held}
}

// Resolve is one dependency, and whether this environment holds it.
func Resolve[A any](services Services) (A, bool) {
	var nothing A
	held, present := services.held[reflect.TypeFor[A]()]
	if !present {
		return nothing, false
	}
	service, sameType := held.(A)
	return service, sameType
}

// Holds reports whether this environment holds a dependency of this type.
func Holds[A any](services Services) bool {
	_, present := Resolve[A](services)
	return present
}

// Named is the types this environment holds, sorted, for a report that has to
// say what a program was given.
func (services Services) Named() []string {
	named := make([]string, 0, len(services.held))
	for held := range services.held {
		named = append(named, held.String())
	}
	sort.Strings(named)
	return named
}

// Missing is a dependency a program needs and was not given.
type Missing struct {
	Wanted string
	Given  []string
}

func (missing Missing) Error() string {
	return "env: no dependency of type " + missing.Wanted + " was provided; this program was given " +
		strings.Join(missing.Given, ", ")
}

// missingOf is the report for a type this environment does not hold.
func missingOf[A any](services Services) Missing {
	return Missing{Wanted: reflect.TypeFor[A]().String(), Given: services.Named()}
}
