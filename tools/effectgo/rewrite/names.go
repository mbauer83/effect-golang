package rewrite

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"
)

// names hands out identifiers no identifier in the file already spells, so a
// generated name can neither shadow nor be shadowed by one the file declares.
type names struct {
	taken map[string]bool
	next  int
}

func newNames(file *ast.File) *names {
	taken := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok {
			taken[ident.Name] = true
		}
		return true
	})
	return &names{taken: taken}
}

func (all *names) fresh(prefix string) string {
	for {
		all.next++
		name := fmt.Sprintf("%s%d", prefix, all.next)
		if !all.taken[name] {
			all.taken[name] = true
			return name
		}
	}
}

// typeNames spells types as the file can write them, adding an import when a
// package the file does not import -- or imports under a name a body shadows
// -- is needed.
type typeNames struct {
	pkg      *types.Package
	names    *names
	imported map[string]string
	added    map[string]string
	// shadowed are the names that might not mean a package where generated
	// code is written: every name declared inside the body being rewritten,
	// and every name in scope at the call that is not a package.
	shadowed func(name string) bool
}

func newTypeNames(file *ast.File, info *types.Info, pkg *types.Package, all *names) *typeNames {
	imported := map[string]string{}
	for _, spec := range file.Imports {
		name := info.PkgNameOf(spec)
		if name == nil || name.Name() == "_" || name.Name() == "." {
			continue
		}
		imported[name.Imported().Path()] = name.Name()
	}
	return &typeNames{pkg: pkg, names: all, imported: imported, added: map[string]string{}}
}

// forSite points shadowing at one call: names declared in its body, and what
// its position resolves each package name to.
func (tn *typeNames) forSite(info *types.Info, found *site) {
	declared := map[string]bool{}
	ast.Inspect(found.literal, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok && info.Defs[ident] != nil {
			declared[ident.Name] = true
		}
		return true
	})
	scope := tn.pkg.Scope().Innermost(found.call.Pos())
	tn.shadowed = func(name string) bool {
		if declared[name] {
			return true
		}
		if scope == nil {
			return false
		}
		_, object := scope.LookupParent(name, found.call.Pos())
		_, isPackage := object.(*types.PkgName)
		return object != nil && !isPackage
	}
}

// packageName is what generated code calls the package at path.
func (tn *typeNames) packageName(path, name string) string {
	if local, ok := tn.imported[path]; ok && !tn.shadowed(local) {
		return local
	}
	if alias, ok := tn.added[path]; ok {
		return alias
	}
	alias := tn.names.fresh("rewritten" + strings.ToUpper(name[:1]) + name[1:])
	tn.added[path] = alias
	return alias
}

// text spells t, or reports that this file cannot: a type unexported from
// another package, or one in an internal package this one may not import.
func (tn *typeNames) text(t types.Type) (string, bool) {
	if !tn.nameable(t, map[types.Type]bool{}) {
		return "", false
	}
	return types.TypeString(t, func(other *types.Package) string {
		if other == tn.pkg {
			return ""
		}
		return tn.packageName(other.Path(), other.Name())
	}), true
}

func (tn *typeNames) nameable(t types.Type, seen map[types.Type]bool) bool {
	if seen[t] {
		return true
	}
	seen[t] = true
	switch t := t.(type) {
	case *types.Named:
		if !tn.visible(t.Obj()) {
			return false
		}
		for i := range t.TypeArgs().Len() {
			if !tn.nameable(t.TypeArgs().At(i), seen) {
				return false
			}
		}
		return true
	case *types.Alias:
		return tn.visible(t.Obj()) && tn.nameable(types.Unalias(t), seen)
	case *types.Pointer:
		return tn.nameable(t.Elem(), seen)
	case *types.Slice:
		return tn.nameable(t.Elem(), seen)
	case *types.Array:
		return tn.nameable(t.Elem(), seen)
	case *types.Chan:
		return tn.nameable(t.Elem(), seen)
	case *types.Map:
		return tn.nameable(t.Key(), seen) && tn.nameable(t.Elem(), seen)
	case *types.Signature:
		return tn.tupleNameable(t.Params(), seen) && tn.tupleNameable(t.Results(), seen)
	case *types.Struct:
		for i := range t.NumFields() {
			if !tn.visible(t.Field(i)) || !tn.nameable(t.Field(i).Type(), seen) {
				return false
			}
		}
		return true
	case *types.Basic, *types.TypeParam, *types.Interface:
		return true
	}
	return false
}

func (tn *typeNames) tupleNameable(tuple *types.Tuple, seen map[types.Type]bool) bool {
	for i := range tuple.Len() {
		if !tn.nameable(tuple.At(i).Type(), seen) {
			return false
		}
	}
	return true
}

func (tn *typeNames) visible(object types.Object) bool {
	other := object.Pkg()
	if other == nil || other == tn.pkg {
		return true
	}
	return object.Exported() && importable(tn.pkg.Path(), other.Path())
}

// importable applies Go's internal-package rule.
func importable(from, path string) bool {
	at := strings.LastIndex(path, "/internal/")
	if at < 0 {
		if !strings.HasSuffix(path, "/internal") {
			return !strings.HasPrefix(path, "internal/")
		}
		at = len(path) - len("/internal")
	}
	parent := path[:at]
	return from == parent || strings.HasPrefix(from, parent+"/")
}
