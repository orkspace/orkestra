package migrate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// buildImportMaps returns alias → importInfo for all imports in one pass,
// replacing the two separate buildImportPathMap / buildImportAliasMap functions.
func buildImportMaps(f *ast.File) map[string]importInfo {
	m := make(map[string]importInfo)
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		var alias string
		if imp.Name != nil && imp.Name.Name != "_" && imp.Name.Name != "." {
			alias = imp.Name.Name
		} else {
			parts := strings.Split(path, "/")
			alias = parts[len(parts)-1]
		}
		m[alias] = importInfo{Path: path, APIVersion: importPathToAPIVersion(path)}
	}
	return m
}

// detectPkgAlias returns the package alias used in a &pkg.Kind{} expression.
// importPathVersion extracts the version segment from an import path when the
// last segment looks like a Go module version tag (e.g. v1, v1alpha1, v2beta2).
func importPathVersion(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return ""
	}
	last := parts[len(parts)-1]
	if len(last) > 1 && last[0] == 'v' && last[1] >= '0' && last[1] <= '9' {
		return last
	}
	return ""
}

// importPathToAPIVersion converts a Go import path to a Kubernetes apiVersion.
// Examples:
//
//	k8s.io/api/apps/v1        → apps/v1
//	k8s.io/api/core/v1        → v1
//	k8s.io/api/networking/v1  → networking/v1
//	github.com/org/project/api/v1alpha1 → TODO: github.com/org/project/api/v1alpha1
func importPathToAPIVersion(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "TODO"
	}
	// k8s.io/api/<group>/<version>
	if strings.HasPrefix(path, "k8s.io/api/") {
		group := parts[len(parts)-2]
		version := parts[len(parts)-1]
		if group == "core" {
			return version
		}
		return group + "/" + version
	}
	// For custom API packages, emit a TODO with the path so the user can fill it in.
	return "TODO: " + path
}

// injectImport inserts a line into the first import block found in src.
func injectImport(src []byte, line string) []byte {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return src
	}
	for _, decl := range f.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok.String() != "import" {
			continue
		}
		insertAt := off(fset, genDecl.Lparen) + 1
		insertion := "\n\t" + line
		return append(src[:insertAt], append([]byte(insertion), src[insertAt:]...)...)
	}
	return src
}
