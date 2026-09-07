package effect

import (
	"math"
	"time"
)

// Jittered scales each continuing delay by a deterministic injected random
// fraction. Fractions outside [0,1] are clamped; NaN selects the minimum.
func (schedule Schedule[In, Out]) Jittered(
	randomFraction func() float64,
	minimumFactor float64,
	maximumFactor float64,
) Schedule[In, Out] {
	minimum, maximum := normalizeJitterBounds(minimumFactor, maximumFactor)
	return Schedule[In, Out]{start: func() scheduleStep[In, Out] {
		return jitteredStep(schedule.driver(), randomFraction, minimum, maximum)
	}}
}

func jitteredStep[In, Out any](
	step scheduleStep[In, Out],
	randomFraction func() float64,
	minimum float64,
	maximum float64,
) scheduleStep[In, Out] {
	return func(now time.Time, input In) (ScheduleDecision[Out], scheduleStep[In, Out]) {
		decision, next := step(now, input)
		if decision.continueRunning {
			fraction := normalizeFraction(randomFraction())
			factor := minimum + fraction*(maximum-minimum)
			decision.delay = scaledDuration(decision.delay, factor)
		}
		return decision, jitteredStep(next, randomFraction, minimum, maximum)
	}
}

func normalizeJitterBounds(minimum float64, maximum float64) (float64, float64) {
	minimum = nonNegativeFactor(minimum)
	maximum = nonNegativeFactor(maximum)
	if maximum < minimum {
		maximum = minimum
	}
	return minimum, maximum
}

func nonNegativeFactor(factor float64) float64 {
	if math.IsNaN(factor) || factor < 0 {
		return 0
	}
	return factor
}

func normalizeFraction(fraction float64) float64 {
	if math.IsNaN(fraction) || fraction < 0 {
		return 0
	}
	if fraction > 1 {
		return 1
	}
	return fraction
}

func scaledDuration(duration time.Duration, factor float64) time.Duration {
	scaled := float64(normalizeDuration(duration)) * factor
	if math.IsInf(scaled, 1) || scaled >= float64(time.Duration(1<<63-1)) {
		return time.Duration(1<<63 - 1)
	}
	return time.Duration(scaled)
}
