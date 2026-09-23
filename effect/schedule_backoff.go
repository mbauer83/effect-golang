package effect

import "time"

// Exponential constructs unlimited doubling backoff capped at maximum.
func Exponential[In any](base time.Duration, maximum time.Duration) Schedule[In, time.Duration] {
	base = normalizeDuration(base)
	maximum = normalizeDuration(maximum)
	if base > maximum {
		base = maximum
	}
	return Schedule[In, time.Duration]{start: func() scheduleStep[In, time.Duration] {
		return exponentialStep[In](base, maximum)
	}}
}

func exponentialStep[In any](delay time.Duration, maximum time.Duration) scheduleStep[In, time.Duration] {
	return func(time.Time, In) (ScheduleDecision[time.Duration], scheduleStep[In, time.Duration]) {
		nextDelay := doubleWithin(delay, maximum)
		return continueSchedule(delay, delay), exponentialStep[In](nextDelay, maximum)
	}
}

// Fibonacci constructs unlimited Fibonacci backoff capped at maximum.
func Fibonacci[In any](one time.Duration, maximum time.Duration) Schedule[In, time.Duration] {
	one = normalizeDuration(one)
	maximum = normalizeDuration(maximum)
	if one > maximum {
		one = maximum
	}
	return Schedule[In, time.Duration]{start: func() scheduleStep[In, time.Duration] {
		return fibonacciStep[In](one, one, maximum)
	}}
}

func fibonacciStep[In any](current time.Duration, next time.Duration, maximum time.Duration) scheduleStep[In, time.Duration] {
	return func(time.Time, In) (ScheduleDecision[time.Duration], scheduleStep[In, time.Duration]) {
		following := addWithin(current, next, maximum)
		return continueSchedule(current, current), fibonacciStep[In](next, following, maximum)
	}
}

func addWithin(left time.Duration, right time.Duration, maximum time.Duration) time.Duration {
	if left >= maximum || right >= maximum-left {
		return maximum
	}
	return left + right
}

func doubleWithin(value time.Duration, maximum time.Duration) time.Duration {
	if value >= maximum || value > maximum-value {
		return maximum
	}
	return value * 2
}
