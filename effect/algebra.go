package effect

// Unit is the neutral product value. It is useful for effects and layers that
// require no environment or produce no meaningful value.
type Unit struct{}

// Never is the uninhabited typed-failure channel used by effects that cannot
// fail with an expected domain error. Its unexported method prevents external
// packages from implementing it.
type Never interface {
	effectNever()
}

// Product is the product of A and B. No information is discarded when two
// values, environments, or results are composed.
type Product[A, B any] struct {
	First  A
	Second B
}

// ProductOf constructs a Product.
func ProductOf[A, B any](first A, second B) Product[A, B] {
	return Product[A, B]{First: first, Second: second}
}

// MapFirst transforms the first component of a Product.
func (p Product[A, B]) MapFirst[C any](f func(A) C) Product[C, B] {
	return Product[C, B]{First: f(p.First), Second: p.Second}
}

// MapSecond transforms the second component of a Product.
func (p Product[A, B]) MapSecond[C any](f func(B) C) Product[A, C] {
	return Product[A, C]{First: p.First, Second: f(p.Second)}
}

// Bimap transforms both components of a Product.
func (p Product[A, B]) Bimap[C, D any](first func(A) C, second func(B) D) Product[C, D] {
	return Product[C, D]{First: first(p.First), Second: second(p.Second)}
}

// Either is an exclusive sum of L and R. It is right-biased: Map and FlatMap
// operate on the Right case and preserve Left.
//
// The zero value is Left(zero L), so Either remains a valid Go zero value.
type Either[L, R any] struct {
	isRight bool
	left    L
	right   R
}

// Left constructs the Left case of Either.
func Left[L, R any](value L) Either[L, R] {
	return Either[L, R]{left: value}
}

// Right constructs the Right case of Either.
func Right[L, R any](value R) Either[L, R] {
	return Either[L, R]{isRight: true, right: value}
}

// IsLeft reports whether e contains a Left value.
func (e Either[L, R]) IsLeft() bool {
	return !e.isRight
}

// IsRight reports whether e contains a Right value.
func (e Either[L, R]) IsRight() bool {
	return e.isRight
}

// LeftValue returns the Left value and true when e is Left.
func (e Either[L, R]) LeftValue() (L, bool) {
	return e.left, !e.isRight
}

// RightValue returns the Right value and true when e is Right.
func (e Either[L, R]) RightValue() (R, bool) {
	return e.right, e.isRight
}

// Fold eliminates Either by handling both cases.
func (e Either[L, R]) Fold[T any](left func(L) T, right func(R) T) T {
	if e.isRight {
		return right(e.right)
	}
	return left(e.left)
}

// Map transforms the Right value.
func (e Either[L, R]) Map[T any](f func(R) T) Either[L, T] {
	if e.isRight {
		return Right[L](f(e.right))
	}
	return Left[L, T](e.left)
}

// FlatMap sequences a right-biased Either computation while keeping L fixed.
func (e Either[L, R]) FlatMap[T any](f func(R) Either[L, T]) Either[L, T] {
	if e.isRight {
		return f(e.right)
	}
	return Left[L, T](e.left)
}

// MapLeft transforms the Left value.
func (e Either[L, R]) MapLeft[M any](f func(L) M) Either[M, R] {
	if e.isRight {
		return Right[M](e.right)
	}
	return Left[M, R](f(e.left))
}

// Bimap transforms either side while preserving the active case.
func (e Either[L, R]) Bimap[M, T any](left func(L) M, right func(R) T) Either[M, T] {
	if e.isRight {
		return Right[M](right(e.right))
	}
	return Left[M, T](left(e.left))
}

// Swap exchanges Left and Right.
func (e Either[L, R]) Swap() Either[R, L] {
	if e.isRight {
		return Left[R, L](e.right)
	}
	return Right[R](e.left)
}
