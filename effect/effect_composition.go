package effect

// Zip combines two independent effects that share R and E, evaluating left
// before right. Like other type-growing combinators it is a package function
// to avoid an unbounded generic method instantiation cycle (golang/go#80172).
func Zip[R, E, A, B any](fx Effect[R, E, A], that Effect[R, E, B]) Effect[R, E, Product[A, B]] {
	return fx.FlatMap(func(left A) Effect[R, E, Product[A, B]] {
		return that.Map(func(right B) Product[A, B] {
			return ProductOf(left, right)
		})
	})
}

// FlatMapMerge composes effects with different R and E channels without
// widening or erasing either channel.
func FlatMapMerge[R, E, A, R2, E2, B any](
	fx Effect[R, E, A],
	f func(A) Effect[R2, E2, B],
) Effect[Product[R, R2], Either[E, E2], B] {
	return asLeftComponent[R2, E2](fx).FlatMap(
		func(value A) Effect[Product[R, R2], Either[E, E2], B] {
			return asRightComponent[R, E](f(value))
		},
	)
}

// ZipMerge combines independent effects with different R and E channels,
// evaluating left before right while preserving all channel information.
func ZipMerge[R, E, A, R2, E2, B any](
	fx Effect[R, E, A],
	that Effect[R2, E2, B],
) Effect[Product[R, R2], Either[E, E2], Product[A, B]] {
	return Zip(asLeftComponent[R2, E2](fx), asRightComponent[R, E](that))
}

// asLeftComponent widens fx into the merged channels of a heterogeneous
// composition, reading its requirements from the product's first component and
// tagging its failures Left.
func asLeftComponent[R2, E2, R, E, A any](fx Effect[R, E, A]) Effect[Product[R, R2], Either[E, E2], A] {
	return fx.
		MapError(Left[E, E2]).
		ContramapEnv(func(env Product[R, R2]) R {
			return env.First
		})
}

// asRightComponent is asLeftComponent for the product's second component.
func asRightComponent[R, E, R2, E2, B any](that Effect[R2, E2, B]) Effect[Product[R, R2], Either[E, E2], B] {
	return that.
		MapError(Right[E, E2]).
		ContramapEnv(func(env Product[R, R2]) R2 {
			return env.Second
		})
}
