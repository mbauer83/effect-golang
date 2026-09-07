package effect

import "time"

// Stop constructs a schedule that never recurs.
func Stop[In any]() Schedule[In, Unit] {
	return Schedule[In, Unit]{start: func() scheduleStep[In, Unit] {
		var step scheduleStep[In, Unit]
		step = func(time.Time, In) (ScheduleDecision[Unit], scheduleStep[In, Unit]) {
			return stopSchedule(Unit{}), step
		}
		return step
	}}
}

// Forever constructs a schedule that recurs immediately and counts from zero.
func Forever[In any]() Schedule[In, uint64] {
	return Schedule[In, uint64]{start: func() scheduleStep[In, uint64] {
		return countingStep[In](0, 0, nil)
	}}
}

// Recurs constructs a schedule with at most count recurrences after the first
// effect execution.
func Recurs[In any](count uint64) Schedule[In, uint64] {
	return Schedule[In, uint64]{start: func() scheduleStep[In, uint64] {
		return countingStep[In](0, 0, &count)
	}}
}

// Spaced constructs an unlimited fixed-delay schedule.
func Spaced[In any](delay time.Duration) Schedule[In, uint64] {
	return Schedule[In, uint64]{start: func() scheduleStep[In, uint64] {
		return countingStep[In](0, normalizeDuration(delay), nil)
	}}
}

func countingStep[In any](count uint64, delay time.Duration, limit *uint64) scheduleStep[In, uint64] {
	return func(time.Time, In) (ScheduleDecision[uint64], scheduleStep[In, uint64]) {
		next := countingStep[In](nextCount(count), delay, limit)
		if limit != nil && count >= *limit {
			return stopSchedule(count), next
		}
		return continueSchedule(count, delay), next
	}
}

const maximumCount = ^uint64(0)

func nextCount(count uint64) uint64 {
	if count == maximumCount {
		return maximumCount
	}
	return count + 1
}

// UpTo continues while elapsed clock time remains below limit.
func UpTo[In any](limit time.Duration) Schedule[In, time.Duration] {
	limit = normalizeDuration(limit)
	return Schedule[In, time.Duration]{start: func() scheduleStep[In, time.Duration] {
		return upToFirstStep[In](limit)
	}}
}

func upToFirstStep[In any](limit time.Duration) scheduleStep[In, time.Duration] {
	return func(now time.Time, _ In) (ScheduleDecision[time.Duration], scheduleStep[In, time.Duration]) {
		return elapsedDecision(0, limit), upToStep[In](now, limit)
	}
}

func upToStep[In any](startedAt time.Time, limit time.Duration) scheduleStep[In, time.Duration] {
	return func(now time.Time, _ In) (ScheduleDecision[time.Duration], scheduleStep[In, time.Duration]) {
		elapsed := normalizeDuration(now.Sub(startedAt))
		return elapsedDecision(elapsed, limit), upToStep[In](startedAt, limit)
	}
}

func elapsedDecision(elapsed time.Duration, limit time.Duration) ScheduleDecision[time.Duration] {
	if elapsed >= limit {
		return stopSchedule(elapsed)
	}
	return continueSchedule(elapsed, 0)
}

func normalizeDuration(duration time.Duration) time.Duration {
	if duration < 0 {
		return 0
	}
	return duration
}
