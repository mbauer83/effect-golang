package rewrite

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Decline is an effect.Gen body left as it was, and why.
type Decline struct {
	Position token.Position
	Reason   string
}

// Result is one file after the rewrite.
type Result struct {
	// Source is the rewritten file; nil when nothing in it was rewritten.
	Source    []byte
	Rewritten int
	Declined  []Decline
}

// File rewrites every effect.Gen body in file that it can translate.
//
// Bodies are rewritten innermost first, so an outer body's replacement is
// written with its inner bodies already replaced; an outer body that is
// declined still has its inner ones rewritten. Only the outermost rewritten
// calls are edits to the file, because their text already holds the rest.
func File(fset *token.FileSet, file *ast.File, info *types.Info, pkg *types.Package, text []byte) Result {
	src := &source{fset: fset, file: fset.File(file.Pos()), text: text}
	src.filename = src.file.Name()

	var calls []*ast.CallExpr
	ast.Inspect(file, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok && isGen(info, call) {
			calls = append(calls, call)
		}
		return true
	})
	slices.Reverse(calls)

	all := newNames(file)
	typeNames := newTypeNames(file, info, pkg, all)
	nested := map[*ast.CallExpr]string{}
	var result Result
	for _, call := range calls {
		replacement, reason := rewriteSite(src, info, typeNames, nested, call)
		if reason != "" {
			result.Declined = append(result.Declined, Decline{Position: fset.Position(call.Pos()), Reason: reason})
			continue
		}
		nested[call] = replacement
		result.Rewritten++
	}
	if result.Rewritten == 0 {
		return result
	}

	var edits []edit
	for call, replacement := range nested {
		if !insideAny(call, nested) {
			edits = append(edits, edit{start: src.offset(call.Pos()), end: src.offset(call.End()), text: replacement})
		}
	}
	edits = append(edits, importEdits(src, file, typeNames)...)
	result.Source = []byte(src.splice(0, len(text), edits))
	return result
}

// rewriteSite answers with the replacement for one call, or why there is none.
// A declined site leaves no import behind.
func rewriteSite(
	src *source,
	info *types.Info,
	typeNames *typeNames,
	nested map[*ast.CallExpr]string,
	call *ast.CallExpr,
) (string, string) {
	found, reason := findSite(info, call)
	if found == nil {
		return "", reason
	}
	if reason := found.unsupported(); reason != "" {
		return "", reason
	}
	before := maps.Clone(typeNames.added)
	typeNames.forSite(info, found)
	em := &emitter{
		src: src, info: info, site: found, names: typeNames.names, types: typeNames,
		nested: nested, emitted: map[*ast.CallExpr]bool{},
	}
	em.effect = typeNames.packageName(effectPath, "effect")
	for _, channel := range []struct {
		t    types.Type
		into *string
	}{{found.r, &em.r}, {found.e, &em.e}, {found.a, &em.a}} {
		spelled, ok := typeNames.text(channel.t)
		if !ok {
			typeNames.added = before
			return "", "a channel has a type this file cannot name"
		}
		*channel.into = spelled
	}
	em.eff = em.effect + ".Effect[" + em.r + ", " + em.e + ", " + em.a + "]"
	body := em.body()
	if em.failure != "" {
		typeNames.added = before
		return "", em.failure
	}
	end := src.fset.Position(call.End())
	return body + fmt.Sprintf("/*line %s:%d:%d*/", src.filename, end.Line, end.Column), ""
}

func insideAny(call *ast.CallExpr, others map[*ast.CallExpr]string) bool {
	for other := range others {
		if other != call && other.Pos() <= call.Pos() && call.End() <= other.End() {
			return true
		}
	}
	return false
}

// importEdits adds the imports generated code needs, on the line of the last
// import so no line below moves.
func importEdits(src *source, file *ast.File, typeNames *typeNames) []edit {
	var last *ast.GenDecl
	for _, decl := range file.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.IMPORT {
			last = gen
		}
	}
	if len(typeNames.added) == 0 || last == nil {
		return nil
	}
	var added strings.Builder
	for _, path := range slices.Sorted(maps.Keys(typeNames.added)) {
		added.WriteString("; import " + typeNames.added[path] + " " + strconv.Quote(path))
	}
	at := src.offset(last.End())
	return []edit{{start: at, end: at, text: added.String()}}
}
