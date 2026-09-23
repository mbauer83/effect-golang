package effect

// Reading the requirements an effect was given.
//
// A Layer builds an environment and Provide satisfies one, and until now
// nothing could ask for it: the only way to reach R was to write a leaf --
// From, Try -- whose function takes it as an argument. That is enough for an
// adapter and not enough for an application. A composed function cannot be
// written against its requirements if reaching them means bottoming out in a
// leaf, so a program that wanted dependencies in R had to thread them as
// arguments instead, and then R was a channel nothing used.
//
// This is the operation that was missing. ZIO spells it environment and
// Effect-TS spells it context; the name here follows the first, because the
// channel is already called R for requirements and Layer already produces an
// environment.

import "context"

// Environment is the requirements this effect was given.
//
//	func filmPage(request Request) answer.Of[Page] {
//	    return effect.Environment[Services, Fault]().
//	        FlatMap(func(services Services) answer.Of[Page] { … })
//	}
//
// A whole environment rather than one service, because R here is one type and
// not a type-indexed map: a program composes what it needs into a struct and
// narrows with ContramapEnv where a part of it will do.
func Environment[R, E any]() Effect[R, E, R] {
	return From(func(_ context.Context, environment R) Exit[E, R] {
		return ExitSuccess[E](environment)
	})
}

// EnvironmentWith is one thing read out of the requirements.
//
//	effect.EnvironmentWith[Services, Fault](func(services Services) catalog.Repository {
//	    return services.Films
//	})
//
// For the common case of wanting one member: it says which at the point of
// use rather than taking the whole environment and reaching into it, so a
// reader sees what this depends on without reading the body.
func EnvironmentWith[R, E, A any](read func(R) A) Effect[R, E, A] {
	return From(func(_ context.Context, environment R) Exit[E, A] {
		return ExitSuccess[E](read(environment))
	})
}

// Anywhere is an effect that requires nothing, run where something is
// required.
//
//	do.Await(effect.Anywhere[Services](catalog.Resolve(kept, source, id)))
//
// A domain that requires nothing is the point of the requirement channel: it
// says, in the type, that resolving a film needs a repository and a source
// and no ambient anything. But composing one with an application step that
// does have requirements means the two disagree about R, and the disagreement
// is not a real one -- an effect needing nothing runs in any environment,
// including the one that has ten dependencies in it.
//
// So this is the widening, named once. Without it every junction between a
// domain effect and an application effect spells out a function discarding an
// environment, which is ceremony that says nothing a reader did not already
// know, and which each program was writing for itself.
func Anywhere[R, E, A any](fx Effect[Unit, E, A]) Effect[R, E, A] {
	return fx.ContramapEnv(func(R) Unit { return Unit{} })
}
