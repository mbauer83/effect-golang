// Package rewrite turns direct-style bodies into FlatMap chains.
//
// The rewrite is an optimisation and never a semantics. A direct.Run body is
// ordinary Go that compiles and runs correctly on its own; this package
// produces a second form of the same program, and only for a body it can
// translate statement by statement. Anything else is left alone and keeps
// running on direct's goroutine, so a body the rewriter declines costs speed
// and nothing else.
package rewrite

import (
	"go/ast"
	"go/token"
	"go/types"
)

const (
	directPath = "github.com/mbauer83/effect-golang/experimental/direct"
	effectPath = "github.com/mbauer83/effect-golang/effect"
)

// site is one direct.Run call and what the rewrite needs to know about it.
type site struct {
	call    *ast.CallExpr
	literal *ast.FuncLit
	do      types.Object
	r, e, a types.Type
	// steps are the body's own do.Await and do.Fail calls, outside any nested
	// function literal.
	steps map[*ast.CallExpr]step
}

// step is one do.Await or do.Fail call.
type step struct {
	fail     bool
	argument ast.Expr
	// value is what an Await answers with; nil for a Fail.
	value types.Type
}

// findSite recognises call as a direct.Run over a function literal whose one
// parameter is a *direct.Do, and answers with its channels and its steps. The
// reason is empty when the call is not a direct.Run at all, and says why when
// it is one this package will not translate.
func findSite(info *types.Info, call *ast.CallExpr) (*site, string) {
	if !isRun(info, call) {
		return nil, ""
	}
	literal, ok := call.Args[0].(*ast.FuncLit)
	if !ok || len(call.Args) != 1 {
		return nil, "the body is not a function literal"
	}
	results := literal.Type.Results
	if results == nil || len(results.List) != 1 || len(results.List[0].Names) > 0 {
		return nil, "the body has named results"
	}
	effectType, ok := info.TypeOf(call).(*types.Named)
	if !ok || effectType.TypeArgs().Len() != 3 {
		return nil, "the call's type is not an effect.Effect"
	}
	arguments := effectType.TypeArgs()
	found := &site{
		call:    call,
		literal: literal,
		r:       arguments.At(0),
		e:       arguments.At(1),
		a:       arguments.At(2),
		steps:   map[*ast.CallExpr]step{},
	}
	params := literal.Type.Params.List
	if len(params) == 1 && len(params[0].Names) == 1 {
		found.do = info.Defs[params[0].Names[0]]
	}
	if reason := found.collectSteps(info); reason != "" {
		return nil, reason
	}
	return found, ""
}

func isRun(info *types.Info, call *ast.CallExpr) bool {
	var ident *ast.Ident
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.SelectorExpr:
		ident = fun.Sel
	case *ast.IndexExpr:
		selector, ok := fun.X.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		ident = selector.Sel
	case *ast.IndexListExpr:
		selector, ok := fun.X.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		ident = selector.Sel
	default:
		return false
	}
	function, ok := info.Uses[ident].(*types.Func)
	return ok && function.Name() == "Run" && function.Pkg() != nil && function.Pkg().Path() == directPath
}

// collectSteps finds every use of do. Each one must be the receiver of an
// Await or Fail call, outside any nested function literal: do is gone after
// the rewrite, so a use of it anywhere else would not compile, and a step
// inside a closure runs at a time the rewrite cannot know.
func (found *site) collectSteps(info *types.Info) string {
	if found.do == nil {
		return ""
	}
	reason := ""
	var visit func(node ast.Node, nested bool) bool
	visit = func(node ast.Node, nested bool) bool {
		if reason != "" {
			return false
		}
		switch node := node.(type) {
		case *ast.FuncLit:
			ast.Inspect(node.Body, func(inner ast.Node) bool { return visit(inner, true) })
			return false
		case *ast.CallExpr:
			selector, ok := ast.Unparen(node.Fun).(*ast.SelectorExpr)
			if !ok || info.Uses[identOf(selector.X)] != found.do {
				return true
			}
			if nested {
				reason = "do is used inside a function literal"
				return false
			}
			switch selector.Sel.Name {
			case "Await":
				found.steps[node] = step{argument: node.Args[0], value: info.TypeOf(node)}
			case "Fail":
				found.steps[node] = step{fail: true, argument: node.Args[0]}
			default:
				reason = "do." + selector.Sel.Name + " is not a step"
				return false
			}
			ast.Inspect(node.Args[0], func(inner ast.Node) bool { return visit(inner, nested) })
			return false
		case *ast.Ident:
			if info.Uses[node] == found.do {
				reason = "do is used other than as the receiver of a step"
				return false
			}
		}
		return true
	}
	ast.Inspect(found.literal.Body, func(node ast.Node) bool { return visit(node, false) })
	return reason
}

func identOf(expr ast.Expr) *ast.Ident {
	ident, _ := ast.Unparen(expr).(*ast.Ident)
	return ident
}

// unsupported names the first construct in the body this package does not
// translate, or answers empty. Loops, defer, go, select and labels are
// declined as a whole rather than one by one: each would need a translation
// of its own, and until one exists the body runs on direct's goroutine.
func (found *site) unsupported() string {
	reason := ""
	ast.Inspect(found.literal.Body, func(node ast.Node) bool {
		if reason != "" {
			return false
		}
		switch node := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.DeferStmt:
			reason = "the body defers a call"
		case *ast.LabeledStmt:
			reason = "the body has a label"
		case *ast.BranchStmt:
			if node.Tok == token.GOTO || node.Tok == token.FALLTHROUGH || node.Label != nil {
				reason = "the body uses " + node.Tok.String()
			}
		case *ast.ForStmt, *ast.RangeStmt, *ast.SelectStmt, *ast.GoStmt:
			if found.containsStep(node) {
				reason = "a step is inside a loop, a select or a go statement"
			}
		case *ast.CallExpr:
			if name := endsGoroutine(node); name != "" {
				reason = "the body calls " + name + ", which a rewritten body would let end the fiber"
			}
		}
		return true
	})
	return reason
}

// endsGoroutine names a call that ends the goroutine it runs on. Direct style
// reports that as a defect of the body; a rewritten body is a FlatMap chain,
// where it ends the fiber's goroutine as it would inside any FlatMap. Only
// calls written in the body can be seen here; one made through a function the
// body calls cannot.
func endsGoroutine(call *ast.CallExpr) string {
	selector, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	switch selector.Sel.Name {
	case "Goexit":
		if ident := identOf(selector.X); ident != nil && ident.Name == "runtime" {
			return "runtime.Goexit"
		}
	case "FailNow", "Fatal", "Fatalf", "SkipNow", "Skip", "Skipf":
		return selector.Sel.Name
	}
	return ""
}

// containsStep reports whether node holds one of this body's steps, outside any
// nested function literal.
func (found *site) containsStep(node ast.Node) bool {
	contains := false
	ast.Inspect(node, func(inner ast.Node) bool {
		if contains {
			return false
		}
		if _, ok := inner.(*ast.FuncLit); ok && inner != ast.Node(found.literal) {
			return false
		}
		if call, ok := inner.(*ast.CallExpr); ok {
			if _, isStep := found.steps[call]; isStep {
				contains = true
			}
		}
		return true
	})
	return contains
}
