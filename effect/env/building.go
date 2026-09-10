package env

// Building a program's environment in groups.
//
// A composition root does not build ten dependencies, it builds a few groups
// of them -- this system's own records, what it reads from somebody else,
// what it keeps for a while -- and some of those groups need a database open
// before they exist. A Layer is how a group is built effectfully and how its
// resources get the lifetime of the program that uses them.
//
// What was missing is composition. ZipLayers answers with Product[ROut,
// ROut2], which is the right shape for two environments that are different
// types and the wrong shape for two that are both Services: a requirement is
// a set, and a set of dependencies zipped with another set is one set, not a
// pair of them. So a group here is a layer from Services to Services -- it
// adds to what it was given -- and composing two of them is composing what
// they add.

import (
	"github.com/mbauer83/effect-golang/effect"
)

// Building is a group of dependencies, built from the ones already provided.
//
// From Services and to Services, because a group that needs a database open
// reads it from what an earlier group provided rather than taking it as a
// type parameter -- and because the result of composing two of these is one
// of these, at every arity, with no shape to project through.
type Building[E any] = effect.Layer[Services, E, Services]

// Adding is a group built by this effect, which reads what it needs from the
// environment built so far and answers with what it adds.
func Adding[E any](build effect.Effect[Services, E, Services]) Building[E] {
	return effect.LayerFromEffect(build)
}

// Held is a group of dependencies that were already built.
//
// For the groups a composition root assembles in ordinary Go -- a handful of
// stores over one open database -- so that they compose with the ones that
// are built effectfully instead of being a second kind of thing.
func Held[E any](services Services) Building[E] {
	return Adding(effect.Succeed[Services, E](services))
}

// Then is one group and then another, the second seeing what the first
// provided.
//
// The failure channel stays as it was rather than growing to Either, because
// two groups of one program's dependencies fail the way that program fails --
// and a root that has to fold an Either at every junction to find out which
// group failed has been handed the framework's bookkeeping instead of an
// answer.
func Then[E any](first Building[E], next Building[E]) Building[E] {
	return Adding(first.Build().FlatMap(func(added Services) effect.Effect[Services, E, Services] {
		return withEverythingSoFar(added, next)
	}))
}

// Assembled is a program's environment, built group by group.
//
// Order says what depends on what -- a group reading an open database goes
// after the group that opens it -- and the environment it answers with does
// not: what comes out is a set, so a step asking for one dependency does not
// know or care which group provided it or in what order.
func Assembled[E any](groups ...Building[E]) Building[E] {
	assembled := Held[E](Empty())
	for _, group := range groups {
		assembled = Then(assembled, group)
	}
	return assembled
}

// withEverythingSoFar runs a group over what has been provided so far and
// answers with all of it, so that Then accumulates rather than replaces.
//
// Both halves matter: a group is given the earlier dependencies because it may
// need them, and the earlier dependencies survive the group because the next
// one may need them too. A layer that only answered with what it added would
// make the last group the whole environment.
func withEverythingSoFar[E any](soFar Services, group Building[E]) effect.Effect[Services, E, Services] {
	return group.Build().
		ContramapEnv(func(Services) Services { return soFar }).
		Map(func(added Services) Services { return soFar.WithAll(added) })
}
