package unit

import (
	effect "github.com/mbauer83/effect-golang"
)

// The programs under test share these channel selections, so each case reads as
// the behaviour it checks rather than as a type expression.
type (
	scopedProgram = effect.Effect[effect.Unit, string, string]
	forkedProgram = effect.Effect[effect.Unit, string, string]
	forkedFiber   = effect.Fiber[string, string]
)
