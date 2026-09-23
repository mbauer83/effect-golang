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
// wherever a branch has to rejoin what follows it. jumps are what an unlabeled
// break or continue becomes, where it leaves a translated switch or loop.
type emitter struct {
	src          *source
	info         *types.Info
	site         *site
	names        *names
	types        *typeNames
	replacements map[*ast.CallExpr]string

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
	out.WriteString(em.list(em.site.literal.Body.List, "", jumpTargets{}))
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
func (em *emitter) list(stmts []ast.Stmt, k string, jumps jumpTargets) string {
	var out strings.Builder
	for i, stmt := range stmts {
		if em.failure != "" {
			return ""
		}
		if !em.needs(stmt) {
			out.WriteString(em.src.line(stmt) + em.text(stmt, nil, jumps) + "\n")
			continue
		}
		out.WriteString(em.statement(stmt, stmts[i+1:], k, jumps))
		return out.String()
	}
	if k != "" && (len(stmts) == 0 || !em.terminates(stmts[len(stmts)-1], jumps)) {
		out.WriteString(k + "\n")
	}
	return out.String()
}

// statement emits one statement that holds a step, and everything after it.
func (em *emitter) statement(stmt ast.Stmt, rest []ast.Stmt, k string, jumps jumpTargets) string {
	if stmts, ok := em.withoutInit(stmt); ok {
		return em.compound(rest, k, jumps, func(next string) string {
			inner := em.list(stmts, next, jumps)
			return "return func() " + em.eff + " {\n" + inner + "}()\n"
		})
	}
	switch node := stmt.(type) {
	case *ast.ForStmt:
		return em.compound(rest, k, jumps, func(next string) string { return em.forLoop(node, next) })
	case *ast.RangeStmt:
		return em.compound(rest, k, jumps, func(next string) string { return em.rangeLoop(node, next) })
	}
	own := em.ownSteps(stmt)
	if em.failure != "" {
		return ""
	}
	if tail, ok := em.tailStep(stmt); ok {
		em.emitted[tail] = true
		return em.chain(own[:len(own)-1], func(temps map[*ast.CallExpr]string) string {
			return "return " + em.src.line(stmt) + em.text(em.site.steps[tail].argument, temps, jumpTargets{}) + "\n"
		})
	}
	switch stmt.(type) {
	case *ast.IfStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.BlockStmt:
		return em.compound(rest, k, jumps, func(next string) string {
			return em.chain(own, func(temps map[*ast.CallExpr]string) string {
				return em.branch(stmt, temps, next, jumps)
			})
		})
	}
	return em.chain(own, func(temps map[*ast.CallExpr]string) string {
		if em.terminates(stmt, jumps) {
			em.unreachable(rest)
			return em.simple(stmt, temps)
		}
		return em.simple(stmt, temps) + em.list(rest, k, jumps)
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

// tailStep recognises return do.Await(fx) where fx answers with the body's own
// type. The awaited effect is then the body's answer as it stands, and
// returning it saves the FlatMap, the closure and the Succeed that awaiting it
// and answering its value would cost.
func (em *emitter) tailStep(stmt ast.Stmt) (*ast.CallExpr, bool) {
	ret, ok := stmt.(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return nil, false
	}
	call, ok := ast.Unparen(ret.Results[0]).(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	step, isStep := em.site.steps[call]
	if !isStep || step.fail || !types.Identical(step.value, em.site.a) {
		return nil, false
	}
	return call, true
}

// compound emits a statement whose branches rejoin what follows it. What
// follows becomes a continuation defined before the statement, so it sees the
// names the statement's own scope would have hidden from it.
func (em *emitter) compound(rest []ast.Stmt, k string, jumps jumpTargets, emit func(next string) string) string {
	if len(rest) == 0 {
		return emit(k)
	}
	name := em.names.fresh("continuation")
	next := "return " + name + "()"
	statement := emit(next)
	if !strings.Contains(statement, next) {
		return statement
	}
	return name + " := func() " + em.eff + " {\n" + em.list(rest, k, jumps) + "}\n" + statement
}

// withoutInit splits an if or switch whose init or header holds a step into
// the init and the statement without it, to be emitted inside a scope of their
// own: the init's names belong to the statement and not to what follows it.
func (em *emitter) withoutInit(stmt ast.Stmt) ([]ast.Stmt, bool) {
	switch node := stmt.(type) {
	case *ast.IfStmt:
		if node.Init != nil && (em.needs(node.Init) || em.headerHasStep(node.Cond)) {
			clone := *node
			clone.Init = nil
			return []ast.Stmt{node.Init, &clone}, true
		}
	case *ast.SwitchStmt:
		if node.Init != nil && (em.needs(node.Init) || em.headerHasStep(node.Tag)) {
			clone := *node
			clone.Init = nil
			return []ast.Stmt{node.Init, &clone}, true
		}
	case *ast.TypeSwitchStmt:
		if node.Init != nil && (em.needs(node.Init) || em.needs(node.Assign)) {
			clone := *node
			clone.Init = nil
			return []ast.Stmt{node.Init, &clone}, true
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
	depth := 0
	for _, call := range own {
		step := em.site.steps[call]
		em.emitted[call] = true
		if step.fail {
			continue
		}
		name := em.names.fresh("awaited")
		value, ok := em.types.text(step.value)
		if !ok {
			em.decline("a step's value has a type this file cannot name")
			return ""
		}
		out.WriteString("return " + em.src.line(call) + em.text(step.argument, temps, jumpTargets{}) +
			".FlatMap(func(" + name + " " + value + ") " + em.eff + " {\n")
		temps[call] = name
		depth++
	}
	out.WriteString(inner(temps))
	out.WriteString(strings.Repeat("})\n", depth))
	return out.String()
}

// simple emits a statement that neither branches nor holds a step of its own
// any more, its steps having been awaited by chain.
func (em *emitter) simple(stmt ast.Stmt, temps map[*ast.CallExpr]string) string {
	if expr, ok := stmt.(*ast.ExprStmt); ok {
		if call, ok := ast.Unparen(expr.X).(*ast.CallExpr); ok {
			if step, isStep := em.site.steps[call]; isStep {
				if step.fail {
					return "return " + em.src.line(stmt) + em.effect + ".Fail[" + em.r + ", " + em.a + ", " + em.e + "](" +
						em.text(step.argument, temps, jumpTargets{}) + ")\n"
				}
				return "_ = " + temps[call] + "\n"
			}
		}
	}
	return em.src.line(stmt) + em.text(stmt, temps, jumpTargets{}) + "\n"
}

// jumpTargets are what an unlabeled break and continue become inside a translated
// switch or loop. Empty where the construct they would leave was not
// translated, so they stay as written.
type jumpTargets struct {
	brk, cont string
}
