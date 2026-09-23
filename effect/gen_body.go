package effect

import (
	"fmt"
	"runtime/debug"
)

// runGenBody runs the body and reports how it ended.
//
// Four endings, told apart in the deferred call because three of them never
// reach the line after body: a return; a failed Await, which ends the
// goroutine with runtime.Goexit; a panic; and a runtime.Goexit of the body's
// own, which is nobody's failure and is reported as a defect.
func runGenBody[R, E, A any](do *Do[R, E], body func(*Do[R, E]) A, ended chan<- Exit[E, A]) {
	returned := false
	var exit Exit[E, A]
	defer func() {
		recovered := recover()
		do.live.Store(false)
		switch {
		case returned:
		case recovered != nil:
			exit = ExitCause[E, A](DieCause[E](Defect{Value: recovered, Stack: string(debug.Stack())}))
		case do.failed:
			exit = ExitCause[E, A](do.cause)
		default:
			exit = ExitCause[E, A](DieCause[E](Defect{
				Value: fmt.Errorf("effect: the Gen body called runtime.Goexit"),
				Stack: string(debug.Stack()),
			}))
		}
		ended <- exit
	}()
	exit = ExitSuccess[E](body(do))
	returned = true
}
