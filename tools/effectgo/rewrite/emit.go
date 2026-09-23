package rewrite

import (
	"go/ast"
	"go/types"
	"strings"
)

// emitter writes one body as a FlatMap chain.
//
// Every emitted statement list ends in a terminating statement, and k is how a
// list that runs off its end continues: empty at the top of the body, where Go
// has already checked that no path runs off, and "return continuationN()"
// wherever a branch has to rejoin what follows it. brk is what an unlabeled
// break in the innermost translated switch becomes.
type emitter struct {
	src    *source
	info   *types.Info
	site   *site
	names  *names
	types  *typeNames
	nested map[*ast.CallExpr]string

	effect  string
	r, e, a string
	eff     string

	emitted map[*ast.CallExpr]bool
	failure string
}

func (em *emitter) decline(reason string) {
	if em.failure == "" {
		em.failure = reason
	}
}

// body is the whole replacement for the Run call.
func (em *emitter) body() string {
	var out strings.Builder
	out.WriteString(em.effect + ".Suspend[" + em.r + ", " + em.e + ", " + em.a + "](func() " + em.eff + " {\n")
	out.WriteString(em.list(em.site.literal.Body.List, "", ""))
	out.WriteString("})")
	for call := range em.site.steps {
		if !em.emitted[call] {
			em.decline("a step is in a position this rewrite does not translate")
		}
	}
	return out.String()
}

func (em *emitter) needs(stmt ast.Stmt) bool { return em.site.containsStep(stmt) }

// list emits stmts, continuing with k when they run off their end.
func (em *emitter) list(stmts []ast.Stmt, k, brk string) string {
	var out strings.Builder
	for i, stmt := range stmts {
		if em.failure != "" {
			return ""
		}
		if !em.needs(stmt) {
			out.WriteString(em.src.line(stmt) + em.text(stmt, nil, brk) + "\n")
			continue
		}
		out.WriteString(em.statement(stmt, stmts[i+1:], k, brk))
		return out.String()
	}
	if k != "" && (len(stmts) == 0 || !em.terminates(stmts[len(stmts)-1], brk)) {
		out.WriteString(k + "\n")
	}
	return out.String()
}

// statement emits one statement that holds a step, and everything after it.
func (em *emitter) statement(stmt ast.Stmt, rest []ast.Stmt, k, brk string) string {
	if wrapped, ok := em.withoutInit(stmt); ok {
		return em.compound(rest, k, brk, func(next string) string {
			inner := em.list(wrapped, next, brk)
			return "return func() " + em.eff + " {\n" + inner + "}()\n"
		})
	}
	own := em.ownSteps(stmt)
	if em.failure != "" {
		return ""
	}
	switch stmt.(type) {
	case *ast.IfStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.BlockStmt:
		return em.compound(rest, k, brk, func(next string) string {
			return em.chain(own, func(temps map[*ast.CallExpr]string) string {
				return em.branching(stmt, temps, next, brk)
			})
		})
	}
	return em.chain(own, func(temps map[*ast.CallExpr]string) string {
		if em.terminates(stmt, brk) {
			em.unreachable(rest)
			return em.simple(stmt, temps)
		}
		return em.simple(stmt, temps) + em.list(rest, k, brk)
	})
}

// unreachable drops what follows a do.Fail. Go needs it written -- Fail does
// not return, but the compiler cannot know that -- and it can never run, so
// its steps count as translated.
func (em *emitter) unreachable(rest []ast.Stmt) {
	for _, stmt := range rest {
		for call := range em.site.steps {
			if stmt.Pos() <= call.Pos() && call.End() <= stmt.End() {
				em.emitted[call] = true
			}
		}
	}
}

// compound emits a statement whose branches rejoin what follows it. What
// follows becomes a continuation defined before the statement, so it sees the
// names the statement's own scope would have hidden from it.
func (em *emitter) compound(rest []ast.Stmt, k, brk string, emit func(next string) string) string {
	if len(rest) == 0 {
		return emit(k)
	}
	name := em.names.fresh("continuation")
	next := "return " + name + "()"
	statement := emit(next)
	if !strings.Contains(statement, next) {
		return statement
	}
	return name + " := func() " + em.eff + " {\n" + em.list(rest, k, brk) + "}\n" + statement
}

// withoutInit splits an if or switch whose init or header holds a step into
// the init and the statement without it, to be emitted inside a scope of their
// own: the init's names belong to the statement and not to what follows it.
func (em *emitter) withoutInit(stmt ast.Stmt) ([]ast.Stmt, bool) {
	switch node := stmt.(type) {
	case *ast.IfStmt:
		if node.Init != nil && (em.needs(node.Init) || em.headerHasStep(node.Cond)) {
			copied := *node
			copied.Init = nil
			return []ast.Stmt{node.Init, &copied}, true
		}
	case *ast.SwitchStmt:
		if node.Init != nil && (em.needs(node.Init) || em.headerHasStep(node.Tag)) {
			copied := *node
			copied.Init = nil
			return []ast.Stmt{node.Init, &copied}, true
		}
	case *ast.TypeSwitchStmt:
		if node.Init != nil && (em.needs(node.Init) || em.needs(node.Assign)) {
			copied := *node
			copied.Init = nil
			return []ast.Stmt{node.Init, &copied}, true
		}
	}
	return nil, false
}

func (em *emitter) headerHasStep(expr ast.Expr) bool {
	return expr != nil && em.site.containsStep(expr)
}

// chain awaits each step in order, each inside the continuation of the one
// before, and emits the statement at the innermost point with each step
// replaced by the value it answered.
func (em *emitter) chain(own []*ast.CallExpr, inner func(map[*ast.CallExpr]string) string) string {
	temps := map[*ast.CallExpr]string{}
	var out strings.Builder
	closing := 0
	for _, call := range own {
		found := em.site.steps[call]
		em.emitted[call] = true
		if found.fail {
			continue
		}
		name := em.names.fresh("awaited")
		value, ok := em.types.text(found.value)
		if !ok {
			em.decline("a step's value has a type this file cannot name")
			return ""
		}
		out.WriteString("return " + em.src.line(call) + em.text(found.argument, temps, "") +
			".FlatMap(func(" + name + " " + value + ") " + em.eff + " {\n")
		temps[call] = name
		closing++
	}
	out.WriteString(inner(temps))
	out.WriteString(strings.Repeat("})\n", closing))
	return out.String()
}

// simple emits a statement that neither branches nor holds a step of its own
// any more, its steps having been awaited by chain.
func (em *emitter) simple(stmt ast.Stmt, temps map[*ast.CallExpr]string) string {
	if expr, ok := stmt.(*ast.ExprStmt); ok {
		if call, ok := ast.Unparen(expr.X).(*ast.CallExpr); ok {
			if found, isStep := em.site.steps[call]; isStep {
				if found.fail {
					return "return " + em.src.line(stmt) + em.effect + ".Fail[" + em.r + ", " + em.a + ", " + em.e + "](" +
						em.text(found.argument, temps, "") + ")\n"
				}
				return "_ = " + temps[call] + "\n"
			}
		}
	}
	return em.src.line(stmt) + em.text(stmt, temps, "") + "\n"
}
