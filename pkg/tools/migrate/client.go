package migrate

import (
	"go/ast"
	"go/token"
	"strconv"
)

// clientFieldKey finds the field on receiverType that has the
// controller-runtime client.Client type and returns the key that must be used
// in a struct literal.
//
// Both named and embedded fields are supported:
//
//	type Reconciler struct {
//	    client.Client
//	}
//
//	=> "Client"
//
//	type Reconciler struct {
//	    k8sClient client.Client
//	}
//
//	=> "k8sClient"
//
// Import aliases are resolved from the import declarations, so all of these
// are supported:
//
//	import "sigs.k8s.io/controller-runtime/pkg/client"
//	import ctrl "sigs.k8s.io/controller-runtime/pkg/client"
//	import crclient "sigs.k8s.io/controller-runtime/pkg/client"
//	import . "sigs.k8s.io/controller-runtime/pkg/client"
//
// Pointer forms are also supported:
//
//	*client.Client
//	*ctrl.Client
//
// It returns ("", false) when no controller-runtime client.Client field exists.
func clientFieldKey(f *ast.File, receiverType string) (string, bool) {
	if f == nil || receiverType == "" {
		return "", false
	}

	clientImport, ok := findCtrlClientImport(f)
	if !ok {
		return "", false
	}

	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}

		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != receiverType {
				continue
			}

			st, ok := ts.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				return "", false
			}

			for _, field := range st.Fields.List {
				if !isControllerRuntimeClientType(field.Type, clientImport) {
					continue
				}

				// Named field:
				//
				//     k8sClient client.Client
				//
				// The struct literal must use the declared field name.
				if len(field.Names) > 0 {
					return field.Names[0].Name, true
				}

				// Embedded field:
				//
				//     client.Client
				//
				// The key is the identifier contributed by the embedded type.
				return embeddedFieldName(field.Type)
			}
		}
	}

	return "", false
}

// ctrlClientImport describes how the controller-runtime client
// package is imported by the source file.
type ctrlClientImport struct {
	// localName is the package identifier used by selector expressions.
	//
	// For:
	//
	//     import "sigs.k8s.io/controller-runtime/pkg/client"
	//
	// it is "client".
	//
	// For:
	//
	//     import ctrl "sigs.k8s.io/controller-runtime/pkg/client"
	//
	// it is "ctrl".
	localName string

	// dot reports whether the package was dot-imported:
	//
	//     import . "sigs.k8s.io/controller-runtime/pkg/client"
	//
	// In that case Client is referenced directly as an identifier.
	dot bool
}

// findCtrlClientImport finds the controller-runtime client import.
//
// It deliberately identifies the package by its import path rather than by
// the local package name. This makes aliases transparent to the migration.
func findCtrlClientImport(f *ast.File) (ctrlClientImport, bool) {
	if f == nil {
		return ctrlClientImport{}, false
	}

	const importPath = "sigs.k8s.io/controller-runtime/pkg/client"

	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != importPath {
			continue
		}

		switch {
		case imp.Name == nil:
			return ctrlClientImport{localName: "client"}, true

		case imp.Name.Name == ".":
			return ctrlClientImport{dot: true}, true

		case imp.Name.Name == "_":
			// A blank import cannot provide client.Client to source code.
			continue

		default:
			return ctrlClientImport{localName: imp.Name.Name}, true
		}
	}

	return ctrlClientImport{}, false
}

// isControllerRuntimeClientType reports whether expr denotes the
// controller-runtime client.Client type using the import information from
// the source file.
//
// Supported:
//
//	client.Client
//	ctrl.Client
//	crclient.Client
//	*client.Client
//	*ctrl.Client
//	Client                    // dot import
func isControllerRuntimeClientType(
	expr ast.Expr,
	clientImport ctrlClientImport,
) bool {
	if !clientImportExists(clientImport) {
		return false
	}

	switch t := expr.(type) {
	case *ast.StarExpr:
		return isControllerRuntimeClientType(t.X, clientImport)

	case *ast.ParenExpr:
		return isControllerRuntimeClientType(t.X, clientImport)

	case *ast.SelectorExpr:
		if clientImport.dot {
			return false
		}

		pkg, ok := t.X.(*ast.Ident)
		if !ok {
			return false
		}

		return pkg.Name == clientImport.localName && t.Sel.Name == "Client"

	case *ast.Ident:
		return clientImport.dot && t.Name == "Client"
	}

	return false
}

func clientImportExists(i ctrlClientImport) bool {
	return i.dot || i.localName != ""
}

// embeddedFieldName returns the struct-literal key contributed by an
// embedded field.
//
// Supported:
//
//	client.Client   -> Client
//	*client.Client  -> Client
//	Client          -> Client
func embeddedFieldName(expr ast.Expr) (string, bool) {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return embeddedFieldName(t.X)
	case *ast.ParenExpr:
		return embeddedFieldName(t.X)
	case *ast.SelectorExpr:
		return t.Sel.Name, true

	case *ast.Ident:
		return t.Name, true

	default:
		return "", false
	}
}
