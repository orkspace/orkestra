package migrate

import (
	"go/ast"
	"go/format"
	"go/token"
	"sort"
	"strings"
)

// findReconcile locates a method named Reconcile whose second parameter is
// a selector expression ending in "Request" (ctrl.Request).
func findReconcile(f *ast.File) *ast.FuncDecl {
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Reconcile" || fn.Recv == nil || fn.Type.Params == nil {
			continue
		}
		params := fn.Type.Params.List
		if len(params) != 2 {
			continue
		}
		sel, ok := params[1].Type.(*ast.SelectorExpr)
		if ok && sel.Sel.Name == "Request" {
			return fn
		}
	}
	return nil
}

// isCtrlResult reports whether expr is ctrl.Result{...} or ctrl.Result{}.
func isCtrlResult(expr ast.Expr) bool {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return false
	}
	sel, ok := lit.Type.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Result"
}

// hasRequeueAfter reports whether a ctrl.Result composite literal sets RequeueAfter.
func hasRequeueAfter(expr ast.Expr) bool {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return false
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if ident, ok := kv.Key.(*ast.Ident); ok && ident.Name == "RequeueAfter" {
			return true
		}
	}
	return false
}

// requeueAfterExpr extracts the RequeueAfter value expression text from a ctrl.Result literal.
// Returns "0" if not found.
func requeueAfterExpr(expr ast.Expr) string {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return "0"
	}
	fset := token.NewFileSet()
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if ident, ok := kv.Key.(*ast.Ident); ok && ident.Name == "RequeueAfter" {
			var buf strings.Builder
			_ = format.Node(&buf, fset, kv.Value)
			return buf.String()
		}
	}
	return "0"
}

// typeName extracts the base type name from a receiver type expression.
// Handles *T and T.
func typeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return typeName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// off returns the byte offset of a token.Pos within fset.
func off(fset *token.FileSet, pos token.Pos) int {
	return fset.Position(pos).Offset
}

// sliceSrc extracts the source bytes for an AST node.
func sliceSrc(src []byte, fset *token.FileSet, n ast.Node) string {
	return string(src[off(fset, n.Pos()):off(fset, n.End())])
}

// applyReplacements applies all replacements to src in reverse offset order
// so earlier offsets remain valid throughout.
func applyReplacements(src []byte, reps []replacement) []byte {
	sort.Slice(reps, func(i, j int) bool {
		return reps[i].start > reps[j].start
	})

	result := make([]byte, len(src))
	copy(result, src)

	for _, r := range reps {
		result = append(result[:r.start], append([]byte(r.text), result[r.end:]...)...)
	}
	return result
}

// setupWithManagerReplacements finds SetupWithManager methods to remove and
// returns the corresponding source replacements and migration warnings.
func setupWithManagerReplacements(fset *token.FileSet, f *ast.File) ([]replacement, []string) {
	var reps []replacement
	var warnings []string

	for _, decl := range f.Decls {
		setupFn, ok := decl.(*ast.FuncDecl)
		if !ok || setupFn.Name.Name != "SetupWithManager" || setupFn.Recv == nil {
			continue
		}

		reps = append(reps, replacement{
			start: off(fset, setupFn.Pos()),
			end:   off(fset, setupFn.End()),
			text: "// SetupWithManager removed — Orkestra provides the informer, workqueue,\n" +
				"// worker pool, leader election, panic recovery, and metrics.\n" +
				"// Delete this file's main.go and scheme registration too.",
		})

		warnings = append(warnings,
			"SetupWithManager removed — delete main.go and scheme registration",
		)
	}

	return reps, warnings
}
