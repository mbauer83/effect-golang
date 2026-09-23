package direct

import (
	"fmt"
	"runtime/debug"

	"github.com/mbauer83/effect-golang/effect"
)

// runBody runs the body and reports how it ended.
//
// Four endings, told apart in the deferred call because three of them never
// reach the line after body: a return; a failed Await, which ends the
// goroutine with runtime.Goexit; a panic; and a runtime.Goexit of the body's
// own, which is nobody's failure and is reported as a defect.
func runBody[R, E, A any](do *Do[R, E], body func(*Do[R, E]) A, ended chan<- effect.Exit[E, A]) {
	returned := false
	var exit effect.Exit[E, A]
	defer func() {
		recovered := recover()
		do.live.Store(false)
		switch {
		case returned:
		case recovered != nil:
			exit = defectExit[E, A](recovered)
		case do.failed:
			exit = effect.ExitCause[E, A](do.cause)
		default:
			exit = defectExit[E, A](fmt.Errorf("direct: the body called runtime.Goexit"))
		}
		ended <- exit
	}()
	exit = effect.ExitSuccess[E](body(do))
	returned = true
}

func defectExit[E, A any](value any) effect.Exit[E, A] {
	return effect.ExitCause[E, A](effect.DieCause[E](effect.Defect{
		Value: value,
		Stack: string(debug.Stack()),
	}))
}
