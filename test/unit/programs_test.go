package unit

import (
	"github.com/mbauer83/effect-golang/effect"
)

// The programs under test share these channel selections, so each case reads as
// the behaviour it checks rather than as a type expression.
type (
	program      = effect.Effect[effect.Unit, string, string]
	programFiber = effect.Fiber[string, string]
)
