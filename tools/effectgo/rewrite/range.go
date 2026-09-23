package rewrite

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// rangeLoop emits a range over an integer, a slice, an array, a pointer to an
// array or a channel. A map, a string and an iterator function are declined:
// a map's iteration tolerates deletion mid-way in a way a snapshot does not,
// a string's decodes runes, and an iterator's yield cannot wait for an effect.
func (em *emitter) rangeLoop(node *ast.RangeStmt, after string) string {
	kind := rangeKind(em.info.TypeOf(node.X))
	if kind == "" {
		em.decline("a step is inside a range over a map, a string or a function")
		return ""
	}
	if kind == "indexed" && node.Value == nil && hasCall(node.X) {
		if _, isSlice := em.info.TypeOf(node.X).Underlying().(*types.Slice); !isSlice {
			em.decline("a range over an array with only a key, which Go does not evaluate")
			return ""
		}
	}
	own := em.ownSteps(&ast.ExprStmt{X: node.X})
	return em.chain(own, func(temps map[*ast.CallExpr]string) string {
		return em.rangeBody(node, kind, em.text(node.X, temps, jumpTargets{}), after)
	})
}

func (em *emitter) rangeBody(node *ast.RangeStmt, kind, over, after string) string {
	collection := em.names.fresh("ranged")
	index := em.names.fresh("index")
	var header, bound string
	indexType := "int"
	switch kind {
	case "integer":
		typeText, ok := em.integerType(node.X)
		if !ok {
			return ""
		}
		indexType = typeText
		header = collection + " := " + indexType + "(" + over + ")\n"
		bound = "if " + index + " >= " + collection + " {\n" + after + "\n}\n"
	case "indexed":
		header = collection + " := " + over + "\n"
		bound = "if " + index + " >= len(" + collection + ") {\n" + after + "\n}\n"
	case "channel":
		header = collection + " := " + over + "\n"
	}
	lp := loop{run: em.names.fresh("iteration"), next: em.names.fresh("advance")}
	if kind != "channel" {
		lp.params = []string{index + " " + indexType}
		lp.args = []string{index}
	}
	cont := "return " + lp.call(lp.next)
	var out strings.Builder
	out.WriteString("return func() " + em.eff + " {\n" + header + em.declare(lp))
	out.WriteString(lp.next + " = " + lp.signature(em.eff) + " {\n")
	if kind != "channel" {
		out.WriteString(index + "++\n")
	}
	out.WriteString(em.again(lp) + "\n}\n")
	out.WriteString(lp.run + " = " + lp.signature(em.eff) + " {\n" + bound)
	out.WriteString(em.src.line(node) + em.iterationVariables(node, kind, collection, index, after))
	out.WriteString(em.list(node.Body.List, cont, jumpTargets{brk: after, cont: cont}))
	start := lp.run + "()"
	if kind != "channel" {
		start = lp.run + "(0)"
	}
	out.WriteString("}\nreturn " + start + "\n}()\n")
	return out.String()
}

// iterationVariables binds this iteration's key and value: declared afresh
// for a :=, assigned for an =.
func (em *emitter) iterationVariables(node *ast.RangeStmt, kind, collection, index, after string) string {
	var values []string
	switch kind {
	case "channel":
		item, open := em.names.fresh("received"), em.names.fresh("open")
		out := item + ", " + open + " := <-" + collection + "\nif !" + open + " {\n" + after + "\n}\n"
		if node.Key == nil {
			return out + "_ = " + item + "\n"
		}
		return out + em.bind(node, []ast.Expr{node.Key}, []string{item})
	case "integer":
		values = []string{index}
	case "indexed":
		values = []string{index, collection + "[" + index + "]"}
	}
	var targets []ast.Expr
	for _, expr := range []ast.Expr{node.Key, node.Value} {
		if expr != nil {
			targets = append(targets, expr)
		}
	}
	if len(targets) == 0 {
		return ""
	}
	return em.bind(node, targets, values[:len(targets)])
}

func (em *emitter) bind(node *ast.RangeStmt, targets []ast.Expr, values []string) string {
	left := make([]string, len(targets))
	used := false
	for i, expr := range targets {
		left[i] = em.text(expr, nil, jumpTargets{})
		if left[i] != "_" {
			used = true
		}
	}
	if !used {
		return ""
	}
	operator := " = "
	if node.Tok == token.DEFINE {
		operator = " := "
	}
	return strings.Join(left, ", ") + operator + strings.Join(values, ", ") + "\n"
}

func (em *emitter) integerType(expr ast.Expr) (string, bool) {
	t := em.info.TypeOf(expr)
	if basic, ok := t.(*types.Basic); ok && basic.Info()&types.IsUntyped != 0 {
		return "int", true
	}
	typeText, ok := em.types.text(t)
	if !ok {
		em.decline("a range's integer has a type this file cannot name")
	}
	return typeText, ok
}

func rangeKind(t types.Type) string {
	switch under := t.Underlying().(type) {
	case *types.Basic:
		if under.Info()&types.IsInteger != 0 {
			return "integer"
		}
	case *types.Slice, *types.Array:
		return "indexed"
	case *types.Pointer:
		if _, ok := under.Elem().Underlying().(*types.Array); ok {
			return "indexed"
		}
	case *types.Chan:
		return "channel"
	}
	return ""
}

func hasCall(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if _, ok := node.(*ast.CallExpr); ok {
			found = true
		}
		return !found
	})
	return found
}
