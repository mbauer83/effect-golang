package rewrite

import (
	"go/ast"
	"strings"
)

// valueTail emits the last step of a body as a Map when nothing after it
// awaits, fails, branches or returns early: the statements that follow and the
// value returned become the mapping, which saves the Succeed a FlatMap's
// continuation would have to wrap the value in -- the chain a person writes.
func (em *emitter) valueTail(stmt ast.Stmt, rest []ast.Stmt, k string, jumps jumpTargets, own []*ast.CallExpr) (string, bool) {
	following := append([]ast.Stmt{stmt}, rest...)
	last, isReturn := following[len(following)-1].(*ast.ReturnStmt)
	if k != "" || jumps != (jumpTargets{}) || !isReturn || len(last.Results) != 1 || !em.endsInValue(own, following) {
		return "", false
	}
	final := own[len(own)-1]
	value, named := em.types.text(em.site.steps[final].value)
	if !named {
		return "", false
	}
	em.emitted[final] = true
	return em.chain(own[:len(own)-1], func(temps map[*ast.CallExpr]string) string {
		name := em.names.fresh("awaited")
		mapped := map[*ast.CallExpr]string{final: name}
		for call, temp := range temps {
			mapped[call] = temp
		}
		var body strings.Builder
		for _, next := range following[:len(following)-1] {
			body.WriteString(em.simple(next, mapped))
		}
		body.WriteString("return " + em.src.line(last) + em.text(last.Results[0], mapped, jumpTargets{}) + "\n")
		return "return " + em.src.line(final) + em.text(em.site.steps[final].argument, temps, jumpTargets{}) +
			".Map(func(" + name + " " + value + ") " + em.a + " {\n" + body.String() + "})\n"
	}), true
}

// endsInValue reports that own awaits a value last, and that nothing in
// following awaits another or leaves early.
func (em *emitter) endsInValue(own []*ast.CallExpr, following []ast.Stmt) bool {
	if len(own) == 0 {
		return false
	}
	for _, call := range own {
		if em.site.steps[call].fail {
			return false
		}
	}
	for _, next := range following[1:] {
		if em.needs(next) {
			return false
		}
	}
	for _, before := range following[:len(following)-1] {
		if returnsOrBranches(before) {
			return false
		}
	}
	return true
}

// returnsOrBranches reports a statement holding a return or a control
// structure outside any function literal, which a Map's body cannot hold as
// written.
func returnsOrBranches(stmt ast.Stmt) bool {
	found := false
	ast.Inspect(stmt, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt, *ast.BranchStmt, *ast.IfStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt,
			*ast.ForStmt, *ast.RangeStmt, *ast.SelectStmt, *ast.BlockStmt, *ast.LabeledStmt, *ast.GoStmt, *ast.DeferStmt:
			found = true
		}
		return !found
	})
	return found
}
