package cases

// Loops whose bodies await, which a rewrite turns into a function per
// iteration.

import (
	"fmt"
	"testing"

	"github.com/mbauer83/effect-golang/experimental/direct"
)

// Every iteration has its own variable, as Go has since 1.22: a closure made
// in one iteration sees that iteration's, after the loop has moved on.
func TestEachIterationHasItsOwnVariables(t *testing.T) {
	value, _ := run(t, direct.Run(func(do *body) string {
		var reads []func() int
		for i := 0; i < 3; i++ {
			reads = append(reads, func() int { return i * do2(i) })
			_ = do.Await(ops.Succeed(i))
		}
		for _, value := range []int{10, 20} {
			reads = append(reads, func() int { return value })
			do.Await(ops.Succeed(value))
		}
		out := ""
		for _, read := range reads {
			out += fmt.Sprint(read(), " ")
		}
		return out
	}))
	want(t, value, "0 1 4 10 20 ")
}

func do2(i int) int { return i }

func TestBreakAndContinueInALoop(t *testing.T) {
	value, _ := run(t, direct.Run(func(do *body) string {
		out := ""
		for i := range 10 {
			n := do.Await(ops.Succeed(i))
			if n%2 == 0 {
				continue
			}
			switch n {
			case 7:
				break
			default:
				out += fmt.Sprint(n)
			}
			if n > 7 {
				break
			}
		}
		return out + "."
	}))
	want(t, value, "1359.")
}

func TestAReturnFromInsideALoop(t *testing.T) {
	value, _ := run(t, direct.Run(func(do *body) string {
		for _, word := range []string{"a", "bb", "ccc"} {
			if len(do.Await(ops.Succeed(word))) == 2 {
				return word
			}
		}
		return "none"
	}))
	want(t, value, "bb")
}

func TestEveryKindOfRange(t *testing.T) {
	value, _ := run(t, direct.Run(func(do *body) int {
		total := 0
		for i := range 3 {
			total += do.Await(ops.Succeed(i))
		}
		array := [2]int{10, 20}
		for _, v := range array {
			total += do.Await(ops.Succeed(v))
		}
		for i := range &array {
			total += do.Await(ops.Succeed(i * 100))
		}
		values := make(chan int, 3)
		values <- 1000
		values <- 2000
		close(values)
		for v := range values {
			total += do.Await(ops.Succeed(v))
		}
		var k, v int
		for k, v = range []int{7, 8} {
			do.Await(ops.Succeed(0))
		}
		return total + k*10000 + v*100000
	}))
	want(t, value, 3+30+100+3000+10000+800000)
}

// A million iterations must not grow the Go stack, rewritten or not.
func TestALongLoop(t *testing.T) {
	value, _ := run(t, direct.Run(func(do *body) int {
		total := 0
		for i := 0; i < 1_000_000; i++ {
			if i%2 == 0 {
				total += do.Await(ops.Succeed(1))
			}
		}
		return total
	}))
	want(t, value, 500_000)
}

// A range over a map is declined and still runs, on direct's goroutine.
func TestADeclinedBodyStillRuns(t *testing.T) {
	value, _ := run(t, direct.Run(func(do *body) int {
		total := 0
		for _, v := range map[string]int{"a": 1, "b": 2} {
			total += do.Await(ops.Succeed(v))
		}
		return total
	}))
	want(t, value, 3)
}
