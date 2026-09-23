package effect

import (
	"context"

	"github.com/mbauer83/effect-golang/effect/internal/lifetime"
	"github.com/mbauer83/effect-golang/effect/internal/outcome"
	runtimecore "github.com/mbauer83/effect-golang/effect/internal/runtime"
)

// ForEach applies f to every input in order and collects the results.
//
// The chain is built for each interpretation, so f is never invoked before the
// effect runs and no accumulator is shared between concurrent runs. The first
// failure short-circuits the rest.
func ForEach[R, E, A, B any](inputs []A, f func(A) Effect[R, E, B]) Effect[R, E, []B] {
	return suspendRuntime(func(context.Context, *runtimecore.State, R) Effect[R, E, []B] {
		accumulator := Succeed[R, E](make([]B, 0, len(inputs)))
		for _, input := range inputs {
			step := f(input)
			accumulator = accumulator.FlatMap(func(values []B) Effect[R, E, []B] {
				return step.Map(func(value B) []B {
					return append(values, value)
				})
			})
		}
		return accumulator
	})
}

// All evaluates every effect in order and collects the results.
func All[R, E, A any](effects []Effect[R, E, A]) Effect[R, E, []A] {
	return ForEach(effects, sameEffect[R, E, A])
}

// ForEachPar applies f to every input concurrently and collects the results in
// input order, however the branches interleave.
//
// Each branch runs in its own fiber inside a private lifetime. The first
// failure cancels the remaining branches, and ForEachPar still waits for every
// branch and its finalizers before completing. Independent failures are
// preserved with Cause.Both in input order.
func ForEachPar[R, E, A, B any](inputs []A, f func(A) Effect[R, E, B]) Effect[R, E, []B] {
	return ForEachParN(inputs, 0, f)
}

// ForEachParN is ForEachPar with at most limit branches running at once. A
// limit of zero or less is unbounded. A branch waiting for a slot remains
// cancelable.
func ForEachParN[R, E, A, B any](inputs []A, limit int, f func(A) Effect[R, E, B]) Effect[R, E, []B] {
	return fromRuntime(func(ctx context.Context, state *runtimecore.State, env R) Exit[E, []B] {
		exits, cleanup := runtimecore.RunAll(
			runtimecore.Interpretation{Context: ctx, State: state, Environment: env},
			parallelBranches(inputs, f, env),
			limit,
			lifetime.ErrSiblingFailed,
		)
		return composeCleanup(collectResults[E, B](exits), cleanup)
	})
}

// AllPar evaluates every effect concurrently and collects the results in input
// order.
func AllPar[R, E, A any](effects []Effect[R, E, A]) Effect[R, E, []A] {
	return ForEachPar(effects, sameEffect[R, E, A])
}

// AllParN is AllPar with at most limit effects running at once.
func AllParN[R, E, A any](effects []Effect[R, E, A], limit int) Effect[R, E, []A] {
	return ForEachParN(effects, limit, sameEffect[R, E, A])
}

func sameEffect[R, E, A any](fx Effect[R, E, A]) Effect[R, E, A] {
	return fx
}

func parallelBranches[R, E, A, B any](
	inputs []A,
	f func(A) Effect[R, E, B],
	env R,
) []runtimecore.Branch {
	branches := make([]runtimecore.Branch, len(inputs))
	for index, input := range inputs {
		branches[index] = eraseWork(f(input), env)
	}
	return branches
}

func collectResults[E, B any](exits []outcome.Exit) Exit[E, []B] {
	results := make([]B, 0, len(exits))
	for _, exit := range exits {
		if !exit.IsSuccess() {
			return Exit[E, []B]{erased: outcome.Failure(
				outcome.CombineBranchCauses(exits, lifetime.ErrSiblingFailed),
			)}
		}
		results = append(results, asValue[B](exit.Value()))
	}
	return ExitSuccess[E](results)
}
