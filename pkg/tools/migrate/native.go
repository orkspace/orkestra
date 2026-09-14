package migrate

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

// rewriteKubeCalls rewrites r.Get/Create/Patch (and r.<field>.Get/Create/Patch)
// to r.kube.* with kubeclient signatures:
//
//	Get(ctx, namespace, name, obj)   — splits the controller-runtime ObjectKey
//	Create(ctx, obj)                 — drops variadic opts
//	Patch(ctx, obj, patch)           — drops variadic opts
func rewriteKubeCalls(src []byte, receiverName string) []byte {
	if receiverName == "" {
		return src
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return src
	}

	var reps []replacement

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		op := sel.Sel.Name
		if op != "Get" && op != "Create" && op != "Patch" {
			return true
		}

		// Match r.Op(...) or r.<field>.Op(...)
		isReceiver := false
		if ident, ok2 := sel.X.(*ast.Ident); ok2 && ident.Name == receiverName {
			isReceiver = true
		} else if outer, ok2 := sel.X.(*ast.SelectorExpr); ok2 {
			if ident, ok3 := outer.X.(*ast.Ident); ok3 && ident.Name == receiverName {
				isReceiver = true
			}
		}
		if !isReceiver {
			return true
		}

		// Rewrite the function selector to r.kube.Op
		reps = append(reps, replacement{
			start: off(fset, call.Fun.Pos()),
			end:   off(fset, call.Fun.End()),
			text:  receiverName + ".kube." + op,
		})

		switch op {
		case "Get":
			// (ctx, ObjectKey{Namespace: X, Name: Y}, obj, opts...) → (ctx, X, Y, obj)
			if len(call.Args) < 3 {
				return true
			}
			ctxText := sliceSrc(src, fset, call.Args[0])
			objText := sliceSrc(src, fset, call.Args[2])
			ns, name := extractObjectKeyFields(src, fset, call.Args[1])
			var newArgs string
			if ns != "" && name != "" {
				newArgs = ctxText + ", " + ns + ", " + name + ", " + objText
			} else {
				keyText := sliceSrc(src, fset, call.Args[1])
				newArgs = ctxText + `, namespace, name, ` + objText +
					` /* TODO(ork migrate): extract namespace+name from: ` + keyText + ` */`
			}
			reps = append(reps, replacement{
				start: off(fset, call.Args[0].Pos()),
				end:   off(fset, call.Args[len(call.Args)-1].End()),
				text:  newArgs,
			})

		case "Create":
			// (ctx, obj, opts...) → (ctx, obj)
			if len(call.Args) > 2 {
				ctxText := sliceSrc(src, fset, call.Args[0])
				objText := sliceSrc(src, fset, call.Args[1])
				reps = append(reps, replacement{
					start: off(fset, call.Args[0].Pos()),
					end:   off(fset, call.Args[len(call.Args)-1].End()),
					text:  ctxText + ", " + objText,
				})
			}

		case "Patch":
			// (ctx, obj, patch, opts...) → (ctx, obj, patch)
			if len(call.Args) > 3 {
				ctxText := sliceSrc(src, fset, call.Args[0])
				objText := sliceSrc(src, fset, call.Args[1])
				patchText := sliceSrc(src, fset, call.Args[2])
				reps = append(reps, replacement{
					start: off(fset, call.Args[0].Pos()),
					end:   off(fset, call.Args[len(call.Args)-1].End()),
					text:  ctxText + ", " + objText + ", " + patchText,
				})
			}
		}
		return true
	})

	return applyReplacements(src, reps)
}

// extractObjectKeyFields extracts Namespace and Name text from a client.ObjectKey composite literal.
func extractObjectKeyFields(src []byte, fset *token.FileSet, n ast.Node) (namespace, name string) {
	lit, ok := n.(*ast.CompositeLit)
	if !ok {
		return "", ""
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		val := sliceSrc(src, fset, kv.Value)
		switch key.Name {
		case "Namespace":
			namespace = val
		case "Name":
			name = val
		}
	}
	return
}

// rewriteStruct finds the reconciler struct by name, replaces its fields with
// Orkestra's (informer, kube, ev), and appends a constructor function if one
// named New<ReceiverType> does not already exist.
func rewriteStruct(src []byte, receiverType string) ([]byte, bool) {
	if receiverType == "" {
		return src, false
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return src, false
	}

	var reps []replacement
	foundStruct := false
	constructorName := "New" + receiverType
	hasConstructor := false

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Name.Name != receiverType {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				foundStruct = true
				reps = append(reps, replacement{
					start: off(fset, st.Fields.Opening),
					end:   off(fset, st.Fields.Closing) + 1,
					text:  "{\n\tkube kubeclient.Interface\n}",
				})
			}
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.Name == constructorName {
				hasConstructor = true
			}
		}
	}

	if !foundStruct {
		return src, false
	}

	result := applyReplacements(src, reps)

	if !hasConstructor {
		constructor := fmt.Sprintf(`
// %s is the constructor function registered in the Katalog.
func %s(kube kubeclient.Interface) domain.Reconciler {
	return &%s{kube: kube}
}
`, constructorName, constructorName, receiverType)
		result = append(result, []byte(constructor)...)
	}

	return result, true
}
