package effect

import "time"

// Schedule is an immutable recurrence policy. Each interpretation creates a
// fresh private driver, so one value may be reused concurrently.
type Schedule[In, Out any] struct {
	start func() scheduleStep[In, Out]
}

type scheduleStep[In, Out any] func(time.Time, In) (ScheduleDecision[Out], scheduleStep[In, Out])

// ScheduleDecision is one output and the decision to stop or continue later.
type ScheduleDecision[Out any] struct {
	output    Out
	delay     time.Duration
	continues bool
}

// Output returns the value emitted by this decision.
func (decision ScheduleDecision[Out]) Output() Out {
	return decision.output
}

// Delay returns the wait before the next execution.
func (decision ScheduleDecision[Out]) Delay() time.Duration {
	return decision.delay
}

// Continues reports whether another execution should occur.
func (decision ScheduleDecision[Out]) Continues() bool {
	return decision.continues
}

func continueSchedule[Out any](output Out, delay time.Duration) ScheduleDecision[Out] {
	if delay < 0 {
		delay = 0
	}
	return ScheduleDecision[Out]{output: output, delay: delay, continues: true}
}

func stopSchedule[Out any](output Out) ScheduleDecision[Out] {
	return ScheduleDecision[Out]{output: output}
}

// ScheduleDriver is one policy's state for one run.
//
// A Schedule value is immutable and safe to reuse concurrently; a driver is
// neither, and must not be shared between runs. Interpretation starts one per
// interpretation, which is what keeps a single policy value reusable, and a
// caller can start one directly to compute a policy's delays without running
// any effects.
type ScheduleDriver[In, Out any] struct {
	step scheduleStep[In, Out]
}

// Start creates a fresh driver for one run of this policy.
func (schedule Schedule[In, Out]) Start() *ScheduleDriver[In, Out] {
	return &ScheduleDriver[In, Out]{step: schedule.driver()}
}

// Next feeds one input observed at now and returns the resulting decision.
func (driver *ScheduleDriver[In, Out]) Next(now time.Time, input In) ScheduleDecision[Out] {
	decision, next := driver.step(now, input)
	driver.step = next
	return decision
}

func (schedule Schedule[In, Out]) driver() scheduleStep[In, Out] {
	if schedule.start == nil {
		panic("effect: zero Schedule has no driver")
	}
	return schedule.start()
}

// MapOutput transforms schedule output without sharing driver state.
func (schedule Schedule[In, Out]) MapOutput[Out2 any](transform func(Out) Out2) Schedule[In, Out2] {
	return Schedule[In, Out2]{start: func() scheduleStep[In, Out2] {
		return mapScheduleStep(schedule.driver(), transform)
	}}
}

func mapScheduleStep[In, Out, Out2 any](step scheduleStep[In, Out], transform func(Out) Out2) scheduleStep[In, Out2] {
	return func(now time.Time, input In) (ScheduleDecision[Out2], scheduleStep[In, Out2]) {
		decision, next := step(now, input)
		result := ScheduleDecision[Out2]{
			output:    transform(decision.output),
			delay:     decision.delay,
			continues: decision.continues,
		}
		return result, mapScheduleStep(next, transform)
	}
}

// WhileInput continues only while predicate accepts the latest input.
func (schedule Schedule[In, Out]) WhileInput(predicate func(In) bool) Schedule[In, Out] {
	return Schedule[In, Out]{start: func() scheduleStep[In, Out] {
		return whileInputStep(schedule.driver(), predicate)
	}}
}

func whileInputStep[In, Out any](step scheduleStep[In, Out], predicate func(In) bool) scheduleStep[In, Out] {
	return func(now time.Time, input In) (ScheduleDecision[Out], scheduleStep[In, Out]) {
		decision, next := step(now, input)
		if !predicate(input) {
			decision.continues = false
			decision.delay = 0
		}
		return decision, whileInputStep(next, predicate)
	}
}

// WhileOutput continues only while predicate accepts the latest policy output.
func (schedule Schedule[In, Out]) WhileOutput(predicate func(Out) bool) Schedule[In, Out] {
	return Schedule[In, Out]{start: func() scheduleStep[In, Out] {
		return whileOutputStep(schedule.driver(), predicate)
	}}
}

func whileOutputStep[In, Out any](step scheduleStep[In, Out], predicate func(Out) bool) scheduleStep[In, Out] {
	return func(now time.Time, input In) (ScheduleDecision[Out], scheduleStep[In, Out]) {
		decision, next := step(now, input)
		if !predicate(decision.output) {
			decision.continues = false
			decision.delay = 0
		}
		return decision, whileOutputStep(next, predicate)
	}
}
