package env

// Resolving a dependency inside an effect.

import (
	"context"

	"github.com/mbauer83/effect-golang/effect"
)

// Resolving carries a program's failure channel so a resolution inherits it,
// the way effect.For does for everything else.
type Resolving[E any] struct{}

// Needing selects the failure channel a program's resolutions are written in.
//
//	needs := env.Needing[fault.Fault]()
//	repository := direct.Bind(bind, needs.Service[catalog.Repository]())
func Needing[E any]() Resolving[E] {
	return Resolving[E]{}
}

// Service is one dependency, resolved from the environment the program was
// given.
//
// A defect when the program was not given one, not a failure. A missing
// dependency is a mis-wired program: no caller can act on it, no retry helps,
// and putting it in the failure channel would make every caller handle a case
// that means the deployment is broken. Complete is how a program finds this
// at start-up instead.
func (Resolving[E]) Service[A any]() effect.Effect[Services, E, A] {
	return Service[A, E]()
}

// Service is one dependency, resolved from the environment the program was
// given.
func Service[A, E any]() effect.Effect[Services, E, A] {
	return effect.From(func(_ context.Context, services Services) effect.Exit[E, A] {
		service, present := Resolve[A](services)
		if !present {
			return effect.ExitCause[E, A](
				effect.DieCause[E](effect.Defect{Value: missingOf[A](services)}))
		}
		return effect.ExitSuccess[E](service)
	})
}
