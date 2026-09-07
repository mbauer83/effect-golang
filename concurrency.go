package effect

import (
	"context"

	runtimecore "github.com/mbauer83/effect-golang/internal/runtime"
)

// ZipPar evaluates both effects concurrently and keeps both results.
//
// Each branch runs in its own fiber inside a private lifetime. If one branch
// fails, defects or is interrupted, the other is canceled and awaited before
// ZipPar completes, so no discarded work is still running. Two genuinely
// independent failures are preserved with Cause.Both in positional order; a
// branch canceled only because its sibling failed is not reported as an
// independent failure.
//
// It is a package function because its success channel is built from the
// branches' own channels (golang/go#80172).
func ZipPar[R, E, A, B any](fx Effect[R, E, A], that Effect[R, E, B]) Effect[R, E, Product[A, B]] {
	return pairing(fx, that, settleOnFailure, runtimecore.ErrSiblingFailed, bothResults[E, A, B])
}

// ZipParMerge evaluates effects with different R and E channels concurrently
// while preserving all channel information.
func ZipParMerge[R, E, A, R2, E2, B any](
	fx Effect[R, E, A],
	that Effect[R2, E2, B],
) Effect[Product[R, R2], Either[E, E2], Product[A, B]] {
	return ZipPar(asLeftComponent[R2, E2](fx), asRightComponent[R, E](that))
}

// Race returns the first successful result.
//
// A branch that fails does not end the race: the other keeps running and may
// still succeed. Only if both fail does Race fail, with Cause.Both of their
// causes. When one succeeds, the loser is canceled and awaited before Race
// completes. Use RaceFirst when the first completion should win regardless of
// its outcome.
func Race[R, E, A any](fx Effect[R, E, A], that Effect[R, E, A]) Effect[R, E, A] {
	return pairing(fx, that, settleOnSuccess, runtimecore.ErrRaceLost, firstSuccess[E, A])
}

// RaceFirst returns the first branch to complete, whether it succeeded or
// failed. It is the direct analogue of selecting on two completion channels.
// The loser is canceled and awaited before RaceFirst completes.
func RaceFirst[R, E, A any](fx Effect[R, E, A], that Effect[R, E, A]) Effect[R, E, A] {
	return pairing(fx, that, settleAlways, runtimecore.ErrRaceLost, firstCompletion[E, A])
}

func settleOnFailure(exit runtimecore.Exit) bool {
	return !exit.Succeeded()
}

func settleOnSuccess(exit runtimecore.Exit) bool {
	return exit.Succeeded()
}

func settleAlways(runtimecore.Exit) bool {
	return true
}

// pairResolver assembles the composition's own exit from both branches'
// terminal exits. induced is the reason this composition uses when it cancels a
// branch, so a resolver can tell an induced interruption from a real failure.
type pairResolver[E, A any] func(outcome runtimecore.PairOutcome, induced error) Exit[E, A]

func pairing[R, E, A, B, C any](
	fx Effect[R, E, A],
	that Effect[R, E, B],
	settle runtimecore.SettlePolicy,
	induced error,
	resolve pairResolver[E, C],
) Effect[R, E, C] {
	return fromRuntime(func(ctx context.Context, state *runtimecore.State, env R) Exit[E, C] {
		outcome, cleanup := runtimecore.RunPair(
			runtimecore.Interpretation{Context: ctx, State: state, Environment: env},
			erasedWork(fx, env),
			erasedWork(that, env),
			settle,
			induced,
		)
		return composeCleanup(resolve(outcome, induced), cleanup)
	})
}

func bothResults[E, A, B any](outcome runtimecore.PairOutcome, induced error) Exit[E, Product[A, B]] {
	if outcome.Left.Succeeded() && outcome.Right.Succeeded() {
		return ExitSuccess[E](ProductOf(
			typedValue[A](outcome.Left.Value()),
			typedValue[B](outcome.Right.Value()),
		))
	}
	return failedPair[E, Product[A, B]](outcome, induced)
}

func firstSuccess[E, A any](outcome runtimecore.PairOutcome, induced error) Exit[E, A] {
	if winner, ok := preferredSuccess(outcome); ok {
		return Exit[E, A]{erased: winner}
	}
	return failedPair[E, A](outcome, induced)
}

// preferredSuccess picks the successful branch, and the earlier completion when
// both succeeded before either could be canceled.
func preferredSuccess(outcome runtimecore.PairOutcome) (runtimecore.Exit, bool) {
	switch {
	case outcome.Left.Succeeded() && outcome.Right.Succeeded():
		if outcome.First == runtimecore.RightSide {
			return outcome.Right, true
		}
		return outcome.Left, true
	case outcome.Left.Succeeded():
		return outcome.Left, true
	case outcome.Right.Succeeded():
		return outcome.Right, true
	default:
		return runtimecore.Exit{}, false
	}
}

func firstCompletion[E, A any](outcome runtimecore.PairOutcome, induced error) Exit[E, A] {
	if outcome.First == runtimecore.RightSide {
		return Exit[E, A]{erased: outcome.Right}
	}
	if outcome.First == runtimecore.LeftSide {
		return Exit[E, A]{erased: outcome.Left}
	}
	return failedPair[E, A](outcome, induced)
}

func failedPair[E, A any](outcome runtimecore.PairOutcome, induced error) Exit[E, A] {
	return Exit[E, A]{erased: runtimecore.Failure(
		runtimecore.CombineParallelCauses(outcome, induced),
	)}
}
