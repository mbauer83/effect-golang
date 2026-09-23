package rewrite

import (
	"go/ast"
	"strings"
)

// branch emits an if, a switch or a block whose branches hold steps. An if
// and a block pass jumps through, because a break inside them still leaves the
// switch around them; a switch is what a break inside it leaves.
func (em *emitter) branch(stmt ast.Stmt, temps map[*ast.CallExpr]string, k string, jumps jumpTargets) string {
	switch node := stmt.(type) {
	case *ast.BlockStmt:
		return "{\n" + em.list(node.List, k, jumps) + "}\n"
	case *ast.IfStmt:
		return em.ifStatement(node, temps, k, jumps)
	case *ast.SwitchStmt:
		header := "switch "
		if node.Init != nil {
			header += em.text(node.Init, temps, jumpTargets{}) + "; "
		}
		if node.Tag != nil {
			header += em.text(node.Tag, temps, jumpTargets{})
		}
		return em.switchStatement(header, node.Body, temps, k, jumps)
	case *ast.TypeSwitchStmt:
		header := "switch "
		if node.Init != nil {
			header += em.text(node.Init, temps, jumpTargets{}) + "; "
		}
		return em.switchStatement(header+em.text(node.Assign, temps, jumpTargets{}), node.Body, temps, k, jumps)
	}
	em.decline("an unexpected statement holds a step")
	return ""
}

func (em *emitter) ifStatement(node *ast.IfStmt, temps map[*ast.CallExpr]string, k string, jumps jumpTargets) string {
	header := "if "
	if node.Init != nil {
		header += em.text(node.Init, temps, jumpTargets{}) + "; "
	}
	out := em.src.line(node) + header + em.text(node.Cond, temps, jumpTargets{}) + " {\n" + em.list(node.Body.List, k, jumps) + "}"
	switch alternative := node.Else.(type) {
	case nil:
		if k != "" {
			out += " else {\n" + k + "\n}"
		}
	case *ast.BlockStmt:
		out += " else {\n" + em.list(alternative.List, k, jumps) + "}"
	case *ast.IfStmt:
		out += " else {\n" + em.list([]ast.Stmt{alternative}, k, jumps) + "}"
	}
	return out + "\n"
}

func (em *emitter) switchStatement(header string, body *ast.BlockStmt, temps map[*ast.CallExpr]string, k string, outer jumpTargets) string {
	var out strings.Builder
	out.WriteString(header + " {\n")
	for _, clause := range body.List {
		clause := clause.(*ast.CaseClause)
		if em.site.containsStep(&ast.BlockStmt{List: exprsAsStmts(clause.List)}) {
			em.decline("a step is in a case expression, which is evaluated only until one matches")
			return ""
		}
		if clause.List == nil {
			out.WriteString("default:\n")
		} else {
			cases := make([]string, len(clause.List))
			for i, expr := range clause.List {
				cases[i] = em.text(expr, temps, jumpTargets{})
			}
			out.WriteString("case " + strings.Join(cases, ", ") + ":\n")
		}
		out.WriteString(em.list(clause.Body, k, jumpTargets{brk: k, cont: outer.cont}))
	}
	out.WriteString("}\n")
	// Every clause ends by continuing with k, so only a switch no clause of
	// which may match -- one without a default -- can run past its end.
	if k != "" && !hasDefault(body) {
		out.WriteString(k + "\n")
	}
	return out.String()
}

func exprsAsStmts(exprs []ast.Expr) []ast.Stmt {
	stmts := make([]ast.Stmt, len(exprs))
	for i, expr := range exprs {
		stmts[i] = &ast.ExprStmt{X: expr}
	}
	return stmts
}

func hasDefault(body *ast.BlockStmt) bool {
	for _, clause := range body.List {
		if clause.(*ast.CaseClause).List == nil {
			return true
		}
	}
	return false
}
