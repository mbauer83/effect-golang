package rewrite

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
)

// ownSteps are the steps in stmt's own expressions -- not in its branches --
// in the order Go evaluates them: an argument before the call it is passed
// to, and otherwise left to right.
//
// Awaiting them before the statement moves them ahead of every other operand,
// which Go allows for operands that are not calls: the spec leaves the order
// between a call and a variable unspecified. A call or a receive written
// before a step would move after it, so that is declined, and so is a step on
// the right of && or ||, which may never be evaluated at all.
func (em *emitter) ownSteps(stmt ast.Stmt) []*ast.CallExpr {
	var exprs []ast.Node
	switch node := stmt.(type) {
	case *ast.ExprStmt:
		exprs = append(exprs, node.X)
	case *ast.AssignStmt:
		for _, expr := range node.Lhs {
			exprs = append(exprs, expr)
		}
		for _, expr := range node.Rhs {
			exprs = append(exprs, expr)
		}
	case *ast.DeclStmt:
		exprs = append(exprs, node)
	case *ast.ReturnStmt:
		for _, expr := range node.Results {
			exprs = append(exprs, expr)
		}
	case *ast.IncDecStmt:
		exprs = append(exprs, node.X)
	case *ast.SendStmt:
		exprs = append(exprs, node.Chan, node.Value)
	case *ast.IfStmt:
		exprs = append(exprs, node.Cond)
		if node.Init != nil {
			exprs = append(exprs, node.Init)
		}
	case *ast.SwitchStmt:
		if node.Tag != nil {
			exprs = append(exprs, node.Tag)
		}
		if node.Init != nil {
			exprs = append(exprs, node.Init)
		}
	case *ast.TypeSwitchStmt:
		exprs = append(exprs, node.Assign)
		if node.Init != nil {
			exprs = append(exprs, node.Init)
		}
	case *ast.BlockStmt:
	default:
		em.decline("a step is in a statement this rewrite does not translate")
		return nil
	}
	var steps []*ast.CallExpr
	var effectful []ast.Node
	for _, expr := range exprs {
		ast.Inspect(expr, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.FuncLit:
				return false
			case *ast.BinaryExpr:
				if (node.Op == token.LAND || node.Op == token.LOR) && em.site.containsStep(node.Y) {
					em.decline("a step is on the right of " + node.Op.String())
				}
			case *ast.UnaryExpr:
				if node.Op == token.ARROW {
					effectful = append(effectful, node)
				}
			case *ast.CallExpr:
				if _, isStep := em.site.steps[node]; isStep {
					steps = append(steps, node)
				} else if !em.pure(node) {
					effectful = append(effectful, node)
				}
			}
			return true
		})
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].End() < steps[j].End() })
	for at, call := range steps {
		for _, other := range effectful {
			encloses := other.Pos() <= call.Pos() && call.End() <= other.End()
			if other.Pos() < call.Pos() && !encloses && !within(other, steps[:at]) {
				em.decline("a call is evaluated before a step in the same statement")
			}
		}
	}
	return steps
}

// within reports whether node is part of one of the earlier steps, which are
// awaited -- argument and all -- before the later ones.
func within(node ast.Node, earlier []*ast.CallExpr) bool {
	for _, call := range earlier {
		if call.Pos() <= node.Pos() && node.End() <= call.End() {
			return true
		}
	}
	return false
}

// pure reports whether a call has no effect a step could observe: a
// conversion, or a builtin that only computes.
func (em *emitter) pure(call *ast.CallExpr) bool {
	if value, ok := em.info.Types[call.Fun]; ok && value.IsType() {
		return true
	}
	ident := identOf(call.Fun)
	if ident == nil {
		return false
	}
	builtin, ok := em.info.Uses[ident].(*types.Builtin)
	if !ok {
		return false
	}
	switch builtin.Name() {
	case "len", "cap", "make", "new", "complex", "real", "imag", "min", "max":
		return true
	}
	return false
}

// terminates reports whether stmt ends the list it is in, so that nothing is
// emitted after it that vet would call unreachable.
func (em *emitter) terminates(stmt ast.Stmt, jumps jumpTargets) bool {
	switch node := stmt.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return node.Tok == token.BREAK && jumps.brk != "" || node.Tok == token.CONTINUE && jumps.cont != ""
	case *ast.ExprStmt:
		call, ok := ast.Unparen(node.X).(*ast.CallExpr)
		if !ok {
			return false
		}
		if step, isStep := em.site.steps[call]; isStep {
			return step.fail
		}
		ident := identOf(call.Fun)
		builtin, isBuiltin := em.info.Uses[ident].(*types.Builtin)
		return ident != nil && isBuiltin && builtin.Name() == "panic"
	case *ast.BlockStmt:
		return len(node.List) > 0 && em.terminates(node.List[len(node.List)-1], jumps)
	case *ast.IfStmt:
		if node.Else == nil || len(node.Body.List) == 0 {
			return false
		}
		return em.terminates(node.Body.List[len(node.Body.List)-1], jumps) && em.terminates(node.Else, jumps)
	case *ast.ForStmt:
		return node.Cond == nil && !breaks(node.Body)
	}
	return false
}

// breaks reports whether an unlabeled break in body leaves the loop around it.
func breaks(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncLit, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
			return false
		case *ast.BranchStmt:
			if node.Tok == token.BREAK {
				found = true
			}
		}
		return !found
	})
	return found
}
