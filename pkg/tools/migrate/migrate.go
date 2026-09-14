// pkg/migrate/migrate.go
//
// Rewrites a controller-runtime Reconcile method to the Orkestra constructor
// signature. It is intentionally a starting point — the output compiles but
// still requires review for status updates, event recording, and informer
// cache lookups.
package migrate

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// Rewrite parses src, locates a controller-runtime Reconcile method, and
// returns the source rewritten according to mode.
//
// ModeToClient is the recommended starting point: zero changes to Reconcile
// or call sites, only SetupWithManager removed and a ToClient constructor
// injected. ModeNative performs the full signature and call-site rewrite.
func Rewrite(src []byte, mode Mode) (*Result, error) {
	if mode == ModeToClient {
		return rewriteToClient(src)
	}
	return rewriteNative(src)
}

// rewriteToClient performs the minimal migration: removes SetupWithManager and
// injects a constructor using orkadapter.ToClient + domain.ReconcilerFrom.
// The Reconcile signature, struct fields, and all call sites are untouched.
func rewriteToClient(src []byte) (*Result, error) {
	fset, f, res, _, err := prepareRewrite(src, ModeToClient)
	if err != nil {
		return nil, err
	}

	setupReps, warnings := setupWithManagerReplacements(fset, f)
	reps := setupReps
	res.Warnings = append(res.Warnings, warnings...)

	result := applyReplacements(src, reps)

	result, constructorWarnings := rewriteConstructor(result, res.ReceiverType)
	res.Warnings = append(res.Warnings, constructorWarnings...)

	result = rewriteImportsToClient(result)

	return finishRewrite(result, res)
}

// rewriteNative performs the full migration: rewrites the Reconcile signature
// and return values, flags status updates, removes SetupWithManager, rewrites
// kube calls, and rewrites the reconciler struct.
func rewriteNative(src []byte) (*Result, error) {
	fset, f, res, fn, err := prepareRewrite(src, ModeNative)
	if err != nil {
		return nil, err
	}

	receiverName := "r"
	if len(fn.Recv.List) > 0 && len(fn.Recv.List[0].Names) > 0 {
		receiverName = fn.Recv.List[0].Names[0].Name
	}

	ctxParam := "ctx"
	params := fn.Type.Params.List
	if len(params) > 0 && len(params[0].Names) > 0 {
		ctxParam = params[0].Names[0].Name
	}

	var reps []replacement

	// Change params to (ctx context.Context, req domain.Request)
	reps = append(reps, replacement{
		start: off(fset, fn.Type.Params.Opening),
		end:   off(fset, fn.Type.Params.Closing) + 1,
		text:  fmt.Sprintf("(%s context.Context, req domain.Request)", ctxParam),
	})

	// Change return type to (domain.Result, error)
	if fn.Type.Results != nil {
		reps = append(reps, replacement{
			start: off(fset, fn.Type.Results.Opening),
			end:   off(fset, fn.Type.Results.Closing) + 1,
			text:  "(domain.Result, error)",
		})
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ReturnStmt:
			if len(x.Results) != 2 || !isCtrlResult(x.Results[0]) {
				return true
			}
			secondText := sliceSrc(src, fset, x.Results[1])
			var text string
			if hasRequeueAfter(x.Results[0]) {
				// Preserve RequeueAfter through domain.Result
				afterExpr := requeueAfterExpr(x.Results[0])
				text = "return domain.Result{RequeueAfter: " + afterExpr + "}, " + secondText
			} else {
				text = "return domain.Result{}, " + secondText
			}
			reps = append(reps, replacement{
				start: off(fset, x.Pos()),
				end:   off(fset, x.End()),
				text:  text,
			})

		}
		return true
	})

	// Flag r.Status().Update() in the whole file
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		upd, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || upd.Sel.Name != "Update" {
			return true
		}
		statusCall, ok := upd.X.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := statusCall.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Status" {
			reps = append(reps, replacement{
				start: off(fset, call.Pos()),
				end:   off(fset, call.End()),
				text:  "nil /* TODO(ork migrate): replace with r.kube.PatchStatus(ctx, obj, map[string]interface{}{...}) */",
			})
			res.Warnings = append(res.Warnings, "r.Status().Update() flagged — replace with r.kube.PatchStatus")
		}
		return true
	})

	setupReps, warnings := setupWithManagerReplacements(fset, f)
	reps = append(reps, setupReps...)
	res.Warnings = append(res.Warnings, warnings...)

	result := applyReplacements(src, reps)

	// Rewrite r.Get/Create/Patch → r.kube.* with kubeclient signatures.
	result = rewriteKubeCalls(result, receiverName)

	// Rewrite struct and inject constructor — parse fresh after replacements.
	result, structFound := rewriteStruct(result, res.ReceiverType)
	if !structFound {
		res.Warnings = append(res.Warnings, "reconciler struct not found — update struct fields and add constructor manually")
	}

	result = rewriteImports(result, false)

	return finishRewrite(result, res)
}

// rewriteImportsToClient injects the domain and kubeclient imports needed by
// the generated constructor. The ctrl import is kept — toclient mode leaves
// the Reconcile signature and body unchanged, so ctrl.Request/ctrl.Result stay.
func rewriteImportsToClient(src []byte) []byte {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return src
	}

	hasDomain, hasKubeclient, hasCtrl := false, false, false
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		switch path {
		case domain:
			hasDomain = true
		case kubeclient:
			hasKubeclient = true
		case ctrlclient:
			hasCtrl = true
		}
	}

	result := src
	if !hasDomain {
		result = injectImport(result, `"`+domain+`"`)
	}
	if !hasKubeclient {
		result = injectImport(result, `"`+kubeclient+`"`)
	}
	if !hasCtrl {
		result = injectImport(result, `"`+ctrlclient+`"`)
	}
	return result
}

// rewriteImports removes the ctrl import and adds strings if needed.
func rewriteImports(src []byte, addStrings bool) []byte {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return src
	}

	var reps []replacement
	hasStrings := false

	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		switch path {
		case "sigs.k8s.io/controller-runtime":
			start := off(fset, imp.Pos())
			end := off(fset, imp.End())
			if end < len(src) && src[end] == '\n' {
				end++
			}
			reps = append(reps, replacement{start: start, end: end, text: ""})
		case "strings":
			hasStrings = true
		}
	}

	result := applyReplacements(src, reps)

	// Inject "strings" import if needed and not already present
	if addStrings && !hasStrings {
		result = injectImport(result, `"strings"`)
	}

	// Inject Orkestra import hints as a block comment so the user knows exactly what to add.
	result = injectImport(result,
		"// TODO(ork migrate): add these imports:\n"+
			"//   \"github.com/orkspace/orkestra/domain\"\n"+
			"//   \"github.com/orkspace/orkestra/pkg/kubeclient\"")

	return result
}
