package rewrite

import (
	"go/ast"
	"go/token"
	"strings"
)

// text is node's source with every edit the rewrite makes inside it:
//
//   - a nested effect.Gen that was itself rewritten is replaced whole;
//   - an awaited step is replaced by the name its value was bound to;
//   - a return answers its value as an effect, because the body now answers
//     with an effect;
//   - an unlabeled break or continue that leaves a translated switch or loop
//     becomes what jumps says it does;
//   - a := that reuses a name declared earlier assigns to it, because the
//     statement may now be in a different block from that declaration, where
//     := would declare a second variable instead of assigning the first.
//
// Inside a function literal only the first applies: its returns and breaks
// belong to it.
func (em *emitter) text(node ast.Node, temps map[*ast.CallExpr]string, jumps jumpTargets) string {
	var edits []edit
	em.collect(node, node, temps, jumps, false, &edits)
	return em.src.splice(em.src.offset(node.Pos()), em.src.offset(node.End()), edits)
}

func (em *emitter) collect(
	root, node ast.Node,
	temps map[*ast.CallExpr]string,
	jumps jumpTargets,
	inFunction bool,
	edits *[]edit,
) {
	jumps = jumpsInside(node, jumps)
	add := func(n ast.Node, text string) {
		*edits = append(*edits, edit{start: em.src.offset(n.Pos()), end: em.src.offset(n.End()), text: text})
	}
	ast.Inspect(node, func(n ast.Node) bool {
		if n == nil || em.failure != "" {
			return false
		}
		switch n := n.(type) {
		case *ast.CallExpr:
			if replacement, ok := em.replacements[n]; ok {
				add(n, replacement)
				return false
			}
			if inFunction {
				return true
			}
			if name, ok := temps[n]; ok {
				add(n, name)
				return false
			}
			if _, isStep := em.site.steps[n]; isStep {
				em.decline("a step was reached before it was awaited")
				return false
			}
		case *ast.FuncLit:
			if n != root {
				em.collect(n.Body, n.Body, temps, jumpTargets{}, true, edits)
				return false
			}
		case *ast.ReturnStmt:
			if !inFunction && len(n.Results) == 1 {
				add(n, "return "+em.effect+".Succeed["+em.r+", "+em.e+", "+em.a+"]("+
					em.text(n.Results[0], temps, jumps)+")")
				return false
			}
		case *ast.BranchStmt:
			if inFunction || n.Label != nil {
				return true
			}
			if n.Tok == token.BREAK && jumps.brk != "" {
				add(n, jumps.brk)
			}
			if n.Tok == token.CONTINUE && jumps.cont != "" {
				add(n, jumps.cont)
			}
		case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
			if n != node && jumps != (jumpTargets{}) {
				em.collect(n, n, temps, jumps, inFunction, edits)
				return false
			}
		case *ast.AssignStmt:
			if !inFunction && n.Tok == token.DEFINE && !isTypeSwitchGuard(n) {
				if replacement, reuses := em.redeclaration(n, temps, jumps); reuses {
					add(n, replacement)
					return false
				}
			}
		}
		return true
	})
}

// redeclaration spells a := that reuses an earlier name as declarations of
// the new names and an assignment to all of them.
func (em *emitter) redeclaration(assign *ast.AssignStmt, temps map[*ast.CallExpr]string, jumps jumpTargets) (string, bool) {
	reuses := false
	var declarations []string
	left := make([]string, len(assign.Lhs))
	for i, expr := range assign.Lhs {
		ident := identOf(expr)
		left[i] = ident.Name
		if ident.Name == "_" {
			continue
		}
		object := em.info.Defs[ident]
		if object == nil {
			reuses = true
			continue
		}
		typeText, ok := em.types.text(object.Type())
		if !ok {
			em.decline("a redeclared name has a type this file cannot name")
			return "", false
		}
		declarations = append(declarations, "var "+ident.Name+" "+typeText+"; ")
	}
	if !reuses {
		return "", false
	}
	right := make([]string, len(assign.Rhs))
	for i, expr := range assign.Rhs {
		right[i] = em.text(expr, temps, jumps)
	}
	return strings.Join(declarations, "") + strings.Join(left, ", ") + " = " + strings.Join(right, ", "), true
}

// isTypeSwitchGuard reports whether assign is the v := x.(type) of a type
// switch, whose v is declared once per clause rather than by the assignment.
func isTypeSwitchGuard(assign *ast.AssignStmt) bool {
	if len(assign.Rhs) != 1 {
		return false
	}
	assertion, ok := ast.Unparen(assign.Rhs[0]).(*ast.TypeAssertExpr)
	return ok && assertion.Type == nil
}

// jumpsInside is what a break and a continue mean inside node: a loop is what both
// of them leave, and a switch or a select is what a break leaves. That holds
// for the node being spelled as much as for one nested inside it.
func jumpsInside(node ast.Node, jumps jumpTargets) jumpTargets {
	switch node.(type) {
	case *ast.ForStmt, *ast.RangeStmt:
		return jumpTargets{}
	case *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
		return jumpTargets{cont: jumps.cont}
	}
	return jumps
}
