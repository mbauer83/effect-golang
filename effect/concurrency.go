package effect

import (
	"context"

	"github.com/mbauer83/effect-golang/effect/internal/lifetime"
	"github.com/mbauer83/effect-golang/effect/internal/outcome"
	runtimecore "github.com/mbauer83/effect-golang/effect/internal/runtime"
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
	return pairResults(fx, that, settleOnFailure, lifetime.ErrSiblingFailed, bothResults[E, A, B])
}

// ZipParChannels evaluates effects with different R and E channels concurrently
// while preserving all channel information.
func ZipParChannels[R, E, A, R2, E2, B any](
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
	return pairResults(fx, that, settleOnSuccess, lifetime.ErrRaceLost, firstSuccess[E, A])
}

// RaceFirst returns the first branch to complete, whether it succeeded or
// failed. It is the direct analogue of selecting on two completion channels.
// The loser is canceled and awaited before RaceFirst completes.
func RaceFirst[R, E, A any](fx Effect[R, E, A], that Effect[R, E, A]) Effect[R, E, A] {
	return pairResults(fx, that, settleAlways, lifetime.ErrRaceLost, firstCompletion[E, A])
}

func settleOnFailure(exit outcome.Exit) bool {
	return !exit.IsSuccess()
}

func settleOnSuccess(exit outcome.Exit) bool {
	return exit.IsSuccess()
}

func settleAlways(outcome.Exit) bool {
	return true
}

// pairResolver assembles the composition's own exit from both branches'
// terminal exits. induced is the reason this composition uses when it cancels a
// branch, so a resolver can tell an induced interruption from a real failure.
type pairResolver[E, A any] func(pair outcome.PairOutcome, cancelReason error) Exit[E, A]

func pairResults[R, E, A, B, C any](
	fx Effect[R, E, A],
	that Effect[R, E, B],
	settle runtimecore.SettlePolicy,
	cancelReason error,
	resolve pairResolver[E, C],
) Effect[R, E, C] {
	return fromRuntime(func(ctx context.Context, state *runtimecore.State, env R) Exit[E, C] {
		pair, cleanup := runtimecore.RunPair(
			runtimecore.Interpretation{Context: ctx, State: state, Environment: env},
			eraseWork(fx, env),
			eraseWork(that, env),
			settle,
			cancelReason,
		)
		return composeCleanup(resolve(pair, cancelReason), cleanup)
	})
}

func bothResults[E, A, B any](pair outcome.PairOutcome, cancelReason error) Exit[E, Product[A, B]] {
	if pair.Left.IsSuccess() && pair.Right.IsSuccess() {
		return ExitSuccess[E](ProductOf(
			asValue[A](pair.Left.Value()),
			asValue[B](pair.Right.Value()),
		))
	}
	return pairFailure[E, Product[A, B]](pair, cancelReason)
}

func firstSuccess[E, A any](pair outcome.PairOutcome, cancelReason error) Exit[E, A] {
	if winner, ok := pickSuccess(pair); ok {
		return Exit[E, A]{erased: winner}
	}
	return pairFailure[E, A](pair, cancelReason)
}

// pickSuccess picks the successful branch, and the earlier completion when
// both succeeded before either could be canceled.
func pickSuccess(pair outcome.PairOutcome) (outcome.Exit, bool) {
	switch {
	case pair.Left.IsSuccess() && pair.Right.IsSuccess():
		if pair.First == outcome.RightSide {
			return pair.Right, true
		}
		return pair.Left, true
	case pair.Left.IsSuccess():
		return pair.Left, true
	case pair.Right.IsSuccess():
		return pair.Right, true
	default:
		return outcome.Exit{}, false
	}
}

func firstCompletion[E, A any](pair outcome.PairOutcome, cancelReason error) Exit[E, A] {
	if pair.First == outcome.RightSide {
		return Exit[E, A]{erased: pair.Right}
	}
	if pair.First == outcome.LeftSide {
		return Exit[E, A]{erased: pair.Left}
	}
	return pairFailure[E, A](pair, cancelReason)
}

func pairFailure[E, A any](pair outcome.PairOutcome, cancelReason error) Exit[E, A] {
	return Exit[E, A]{erased: outcome.Failure(
		outcome.CombineParallelCauses(pair, cancelReason),
	)}
}
