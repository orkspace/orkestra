package migrate

import (
	"go/ast"
)

// detectKindAndAlias extracts the struct name and package alias from a
// &pkg.Kind{} or &Kind{} expression. alias is empty when there is no qualifier.
func detectKindAndAlias(arg ast.Expr) (kind, alias string) {
	if unary, ok := arg.(*ast.UnaryExpr); ok {
		arg = unary.X
	}
	lit, ok := arg.(*ast.CompositeLit)
	if !ok {
		return "", ""
	}
	switch t := lit.Type.(type) {
	case *ast.SelectorExpr:
		if ident, ok := t.X.(*ast.Ident); ok {
			return t.Sel.Name, ident.Name
		}
	case *ast.Ident:
		return t.Name, ""
	}
	return "", ""
}

// detectTypeArg extracts Kind and APIVersion from a &pkg.Kind{} argument.
func detectTypeArg(arg ast.Expr, imports map[string]importInfo) (DetectedType, bool) {
	kind, alias := detectKindAndAlias(arg)
	if kind == "" {
		return DetectedType{}, false
	}
	apiVersion := "TODO"
	if alias != "" {
		apiVersion = imports[alias].APIVersion
	}
	return DetectedType{Kind: kind, APIVersion: apiVersion}, true
}

// extractOwnsWatches scans SetupWithManager for For(), Owns(), and Watches() call
// chains. Import aliases are resolved to apiVersion strings and import paths using
// the file's import declarations (best-effort; emits TODO when unresolvable).
func extractOwnsWatches(f *ast.File) (primary PrimaryType, owns, watches []DetectedType) {
	imports := buildImportMaps(f)

	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "SetupWithManager" || fn.Recv == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			method := sel.Sel.Name
			if len(call.Args) == 0 {
				return true
			}
			switch method {
			case "For":
				kind, pkgAlias := detectKindAndAlias(call.Args[0])
				if kind == "" {
					return true
				}
				info := imports[pkgAlias]
				primary = PrimaryType{
					Kind:       kind,
					Object:     kind,
					ObjectList: kind + "List",
					Version:    importPathVersion(info.Path),
					Location:   info.Path,
					Alias:      pkgAlias,
				}
			case "Owns":
				dt, ok := detectTypeArg(call.Args[0], imports)
				if !ok {
					return true
				}
				owns = append(owns, dt)
			case "Watches":
				dt, ok := detectTypeArg(call.Args[0], imports)
				if !ok {
					return true
				}
				watches = append(watches, dt)
			}
			return true
		})
	}
	return
}
