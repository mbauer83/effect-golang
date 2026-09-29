package effect

// Running effects of different types at once into the fields of one struct:
// the shape effect-ts gives Effect.all over an object. Named fields where
// ZipPar would nest a Product per effect.

// Field is one effect whose value is assigned to a field of S.
type Field[R, E, S any] struct {
	assignment Effect[R, E, func(*S)]
}

// FieldOf runs fx and assigns its value to a field of S.
//
//	effect.FieldOf(tracking(viewer), func(page *Page, tracked Tracking) { page.Tracking = tracked })
func FieldOf[R, E, S, A any](fx Effect[R, E, A], assign func(*S, A)) Field[R, E, S] {
	return Field[R, E, S]{assignment: fx.Map(func(value A) func(*S) {
		return func(into *S) { assign(into, value) }
	})}
}

// StructPar runs every field's effect at once and assigns their values, in
// the order given, to one S. The first failure interrupts the others, as
// AllPar does.
func StructPar[R, E, S any](fields ...Field[R, E, S]) Effect[R, E, S] {
	assignments := make([]Effect[R, E, func(*S)], len(fields))
	for i, field := range fields {
		assignments[i] = field.assignment
	}
	return AllPar(assignments).Map(func(assigners []func(*S)) S {
		var value S
		for _, assign := range assigners {
			assign(&value)
		}
		return value
	})
}
