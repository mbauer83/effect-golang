package rewrite

import (
	"go/ast"
	"go/token"
	"strings"
)

// A loop whose body awaits becomes a function called once per iteration,
// and its back edge goes through Suspend, so a million iterations add no Go
// stack. The variables a loop declares are that function's parameters, which
// is what gives every iteration its own, as Go has since 1.22: a closure made
// in one iteration keeps seeing that iteration's variable.
//
// break continues with what follows the loop, continue with the next
// iteration, and falling off the body is continuing.

// loop is the function names one loop is emitted with.
type loop struct {
	run, next string
	params    []string // "name type"
	args      []string // "name"
}

func (em *emitter) newLoop(vars []*ast.Ident) (loop, bool) {
	result := loop{run: em.names.fresh("iteration"), next: em.names.fresh("advance")}
	for _, ident := range vars {
		object := em.info.Defs[ident]
		if object == nil || ident.Name == "_" {
			continue
		}
		typeText, ok := em.types.text(object.Type())
		if !ok {
			em.decline("a loop variable has a type this file cannot name")
			return loop{}, false
		}
		result.params = append(result.params, ident.Name+" "+typeText)
		result.args = append(result.args, ident.Name)
	}
	return result, true
}

func (lp loop) signature(eff string) string {
	return "func(" + strings.Join(lp.params, ", ") + ") " + eff
}

func (lp loop) call(name string) string {
	return name + "(" + strings.Join(lp.args, ", ") + ")"
}

// declare is the two functions, declared before either is defined because
// each calls the other.
func (em *emitter) declare(lp loop) string {
	return "var " + lp.run + ", " + lp.next + " " + lp.signature(em.eff) + "\n"
}

// again is the back edge: the next iteration, as an effect rather than a call,
// so it runs from the interpreter's loop and not from this one's stack.
func (em *emitter) again(lp loop) string {
	return "return " + em.effect + ".Suspend[" + em.r + ", " + em.e + ", " + em.a + "](func() " + em.eff +
		" { return " + lp.call(lp.run) + " })"
}

// forLoop emits a three-clause or condition-only loop, and continues with
// after once it ends.
func (em *emitter) forLoop(node *ast.ForStmt, after string) string {
	for _, part := range []ast.Node{node.Init, node.Cond, node.Post} {
		if part != nil && em.site.containsStep(part) {
			em.decline("a step is in a loop's init, condition or post statement")
			return ""
		}
	}
	var vars []*ast.Ident
	if assign, ok := node.Init.(*ast.AssignStmt); ok && assign.Tok == token.DEFINE {
		for _, expr := range assign.Lhs {
			vars = append(vars, identOf(expr))
		}
	}
	lp, ok := em.newLoop(vars)
	if !ok {
		return ""
	}
	cont := "return " + lp.call(lp.next)
	var out strings.Builder
	out.WriteString("return func() " + em.eff + " {\n")
	if node.Init != nil {
		out.WriteString(em.src.line(node.Init) + em.text(node.Init, nil, jumpTargets{}) + "\n")
	}
	out.WriteString(em.declare(lp))
	out.WriteString(lp.next + " = " + lp.signature(em.eff) + " {\n")
	if node.Post != nil {
		out.WriteString(em.src.line(node.Post) + em.text(node.Post, nil, jumpTargets{}) + "\n")
	}
	out.WriteString(em.again(lp) + "\n}\n")
	out.WriteString(lp.run + " = " + lp.signature(em.eff) + " {\n")
	if node.Cond != nil {
		out.WriteString("if !(" + em.src.line(node.Cond) + em.text(node.Cond, nil, jumpTargets{}) + ") {\n" + after + "\n}\n")
	}
	out.WriteString(em.list(node.Body.List, cont, jumpTargets{brk: after, cont: cont}))
	out.WriteString("}\nreturn " + lp.call(lp.run) + "\n}()\n")
	return out.String()
}
