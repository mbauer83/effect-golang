package effect

import "time"

// AndSchedules intersects two schedules. Both must continue, and the longer
// delay wins because both timing constraints must be satisfied.
func AndSchedules[In, Left, Right any](left Schedule[In, Left], right Schedule[In, Right]) Schedule[In, Product[Left, Right]] {
	return Schedule[In, Product[Left, Right]]{start: func() scheduleStep[In, Product[Left, Right]] {
		return andScheduleStep(left.driver(), right.driver())
	}}
}

func andScheduleStep[In, Left, Right any](left scheduleStep[In, Left], right scheduleStep[In, Right]) scheduleStep[In, Product[Left, Right]] {
	return func(now time.Time, input In) (ScheduleDecision[Product[Left, Right]], scheduleStep[In, Product[Left, Right]]) {
		leftDecision, nextLeft := left(now, input)
		rightDecision, nextRight := right(now, input)
		output := ProductOf(leftDecision.output, rightDecision.output)
		next := andScheduleStep(nextLeft, nextRight)
		if !leftDecision.continueRunning || !rightDecision.continueRunning {
			return stopSchedule(output), next
		}
		return continueSchedule(output, max(leftDecision.delay, rightDecision.delay)), next
	}
}

// ScheduleUnion is the output of OrSchedules. Continues fields report which
// component requested another recurrence for the current decision.
type ScheduleUnion[Left, Right any] struct {
	Left           Left
	Right          Right
	LeftContinues  bool
	RightContinues bool
}

// OrSchedules unions two schedules. Either may continue, and the shortest
// delay among the continuing components wins.
func OrSchedules[In, Left, Right any](left Schedule[In, Left], right Schedule[In, Right]) Schedule[In, ScheduleUnion[Left, Right]] {
	return Schedule[In, ScheduleUnion[Left, Right]]{start: func() scheduleStep[In, ScheduleUnion[Left, Right]] {
		return orScheduleStep(left.driver(), right.driver())
	}}
}

func orScheduleStep[In, Left, Right any](
	left scheduleStep[In, Left],
	right scheduleStep[In, Right],
) scheduleStep[In, ScheduleUnion[Left, Right]] {
	return func(now time.Time, input In) (ScheduleDecision[ScheduleUnion[Left, Right]], scheduleStep[In, ScheduleUnion[Left, Right]]) {
		leftDecision, nextLeft := left(now, input)
		rightDecision, nextRight := right(now, input)
		output := ScheduleUnion[Left, Right]{
			Left:           leftDecision.output,
			Right:          rightDecision.output,
			LeftContinues:  leftDecision.continueRunning,
			RightContinues: rightDecision.continueRunning,
		}
		next := orScheduleStep(nextLeft, nextRight)
		if !output.LeftContinues && !output.RightContinues {
			return stopSchedule(output), next
		}
		return continueSchedule(output, activeMinimumDelay(leftDecision, rightDecision)), next
	}
}

func activeMinimumDelay[Left, Right any](left ScheduleDecision[Left], right ScheduleDecision[Right]) time.Duration {
	if !left.continueRunning {
		return right.delay
	}
	if !right.continueRunning {
		return left.delay
	}
	return min(left.delay, right.delay)
}
