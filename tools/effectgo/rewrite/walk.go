package rewrite

import (
	"go/ast"
	"go/token"
	"strings"
)

// text is node's source with every edit the rewrite makes inside it:
//
//   - a nested direct.Run that was itself rewritten is replaced whole;
//   - an awaited step is replaced by the name its value was bound to;
//   - a return answers its value as an effect, because the body now answers
//     with an effect;
//   - an unlabeled break that leaves a translated switch continues with brk;
//   - a := that reuses a name declared earlier assigns to it, because the
//     statement may now be in a different block from that declaration, where
//     := would declare a second variable instead of assigning the first.
//
// Inside a function literal only the first applies: its returns and breaks
// belong to it.
func (em *emitter) text(node ast.Node, temps map[*ast.CallExpr]string, brk string) string {
	var edits []edit
	em.collect(node, node, temps, brk, false, &edits)
	return em.src.splice(em.src.offset(node.Pos()), em.src.offset(node.End()), edits)
}

func (em *emitter) collect(
	root, node ast.Node,
	temps map[*ast.CallExpr]string,
	brk string,
	inFunction bool,
	edits *[]edit,
) {
	add := func(n ast.Node, text string) {
		*edits = append(*edits, edit{start: em.src.offset(n.Pos()), end: em.src.offset(n.End()), text: text})
	}
	ast.Inspect(node, func(n ast.Node) bool {
		if n == nil || em.failure != "" {
			return false
		}
		switch n := n.(type) {
		case *ast.CallExpr:
			if replacement, ok := em.nested[n]; ok {
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
				em.collect(n.Body, n.Body, temps, "", true, edits)
				return false
			}
		case *ast.ReturnStmt:
			if !inFunction && len(n.Results) == 1 {
				add(n, "return "+em.effect+".Succeed["+em.r+", "+em.e+", "+em.a+"]("+
					em.text(n.Results[0], temps, brk)+")")
				return false
			}
		case *ast.BranchStmt:
			if !inFunction && n.Tok == token.BREAK && n.Label == nil && brk != "" {
				add(n, brk)
			}
		case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
			if n != root && brk != "" {
				em.collect(n, n, temps, "", inFunction, edits)
				return false
			}
		case *ast.AssignStmt:
			if !inFunction && n.Tok == token.DEFINE && !isTypeSwitchGuard(n) {
				if replacement, reuses := em.redeclaration(n, temps, brk); reuses {
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
func (em *emitter) redeclaration(assign *ast.AssignStmt, temps map[*ast.CallExpr]string, brk string) (string, bool) {
	reuses := false
	var declared []string
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
		declared = append(declared, "var "+ident.Name+" "+typeText+"; ")
	}
	if !reuses {
		return "", false
	}
	right := make([]string, len(assign.Rhs))
	for i, expr := range assign.Rhs {
		right[i] = em.text(expr, temps, brk)
	}
	return strings.Join(declared, "") + strings.Join(left, ", ") + " = " + strings.Join(right, ", "), true
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
