package rewrite

import (
	"go/ast"
	"strings"
)

// branching emits an if, a switch or a block whose branches hold steps. An if
// and a block pass brk through, because a break inside them still leaves the
// switch around them; a switch is what a break inside it leaves.
func (em *emitter) branching(stmt ast.Stmt, temps map[*ast.CallExpr]string, k, brk string) string {
	switch node := stmt.(type) {
	case *ast.BlockStmt:
		return "{\n" + em.list(node.List, k, brk) + "}\n"
	case *ast.IfStmt:
		return em.ifStatement(node, temps, k, brk)
	case *ast.SwitchStmt:
		header := "switch "
		if node.Init != nil {
			header += em.text(node.Init, temps, "") + "; "
		}
		if node.Tag != nil {
			header += em.text(node.Tag, temps, "")
		}
		return em.switchStatement(header, node.Body, temps, k)
	case *ast.TypeSwitchStmt:
		header := "switch "
		if node.Init != nil {
			header += em.text(node.Init, temps, "") + "; "
		}
		return em.switchStatement(header+em.text(node.Assign, temps, ""), node.Body, temps, k)
	}
	em.decline("an unexpected statement holds a step")
	return ""
}

func (em *emitter) ifStatement(node *ast.IfStmt, temps map[*ast.CallExpr]string, k, brk string) string {
	header := "if "
	if node.Init != nil {
		header += em.text(node.Init, temps, "") + "; "
	}
	out := em.src.line(node) + header + em.text(node.Cond, temps, "") + " {\n" + em.list(node.Body.List, k, brk) + "}"
	switch alternative := node.Else.(type) {
	case nil:
		if k != "" {
			out += " else {\n" + k + "\n}"
		}
	case *ast.BlockStmt:
		out += " else {\n" + em.list(alternative.List, k, brk) + "}"
	case *ast.IfStmt:
		out += " else {\n" + em.list([]ast.Stmt{alternative}, k, brk) + "}"
	}
	return out + "\n"
}

func (em *emitter) switchStatement(header string, body *ast.BlockStmt, temps map[*ast.CallExpr]string, k string) string {
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
				cases[i] = em.text(expr, temps, "")
			}
			out.WriteString("case " + strings.Join(cases, ", ") + ":\n")
		}
		out.WriteString(em.list(clause.Body, k, k))
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
