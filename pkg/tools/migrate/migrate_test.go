package migrate

import (
	"strings"
	"testing"
)

// baseline is a minimal controller-runtime reconciler matching the pattern
// that ork migrate targets — embedded client, ctrl.Request signature, req.NamespacedName.
const baseline = `package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	demov1alpha1 "github.com/example/operator/api/v1alpha1"
)

type WebAppReconciler struct {
	client.Client
	Scheme interface{}
}

func (r *WebAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	webapp := &demov1alpha1.WebApp{}
	if err := r.Get(ctx, req.NamespacedName, webapp); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	webapp.Status.Phase = "Running"
	if err := r.Status().Update(ctx, webapp); err != nil {
		logger.Error(err, "status update failed")
		return ctrl.Result{}, err
	}

	_ = fmt.Sprintf("reconciled %s", req.String())
	return ctrl.Result{}, nil
}

func (r *WebAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&demov1alpha1.WebApp{}).
		Complete(r)
}
`

func TestRewrite_SignatureChange(t *testing.T) {
	res, err := Rewrite([]byte(baseline), ModeNative)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	src := string(res.Source)

	if !strings.Contains(src, "Reconcile(ctx context.Context, req domain.Request) (domain.Result, error)") {
		t.Error("expected Orkestra signature: Reconcile(ctx context.Context, req domain.Request) (domain.Result, error)")
	}
	if strings.Contains(src, "ctrl.Request") {
		t.Error("ctrl.Request should be removed")
	}
	if strings.Contains(src, "ctrl.Result") {
		t.Error("ctrl.Result should be removed")
	}
}

func TestRewrite_ReturnCollapse(t *testing.T) {
	res, err := Rewrite([]byte(baseline), ModeNative)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	src := string(res.Source)

	if strings.Contains(src, "ctrl.Result{}") {
		t.Error("ctrl.Result{} should be replaced with domain.Result{}")
	}
	// Error returns become `return domain.Result{}, err`, nil returns become `return domain.Result{}, nil`.
	if !strings.Contains(src, "return domain.Result{}, err") {
		t.Error("expected return domain.Result{}, err")
	}
	if !strings.Contains(src, "return domain.Result{}, nil") {
		t.Error("expected return domain.Result{}, nil")
	}
}

func TestRewrite_ReqNamespacedName(t *testing.T) {
	res, err := Rewrite([]byte(baseline), ModeNative)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	src := string(res.Source)

	// No key-split injection — domain.Request carries NamespacedName directly.
	if strings.Contains(src, `strings.SplitN`) {
		t.Error("key-split injection should not be emitted")
	}
	// rewriteKubeCalls rewrites the call site with a TODO comment since
	// req.NamespacedName is not a composite literal it can decompose automatically.
	if !strings.Contains(src, "r.kube.Get") {
		t.Error("expected r.kube.Get rewrite")
	}
}

func TestRewrite_ReqString(t *testing.T) {
	res, err := Rewrite([]byte(baseline), ModeNative)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	src := string(res.Source)

	// req.String() is preserved — domain.Request implements Stringer
	if !strings.Contains(src, "req.String()") {
		t.Error("req.String() should be preserved — domain.Request has String()")
	}
}

func TestRewrite_SetupWithManagerRemoved(t *testing.T) {
	res, err := Rewrite([]byte(baseline), ModeNative)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	src := string(res.Source)

	// The method declaration must be gone; only the replacement comment remains.
	if strings.Contains(src, "func (r *WebAppReconciler) SetupWithManager") {
		t.Error("SetupWithManager func declaration should be removed")
	}
	if !strings.Contains(src, "SetupWithManager removed") {
		t.Error("expected SetupWithManager removal comment")
	}
}

func TestRewrite_StructRewritten(t *testing.T) {
	res, err := Rewrite([]byte(baseline), ModeNative)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	src := string(res.Source)

	if strings.Contains(src, "client.Client") {
		t.Error("embedded client.Client should be removed from struct")
	}
	if !strings.Contains(src, "kube kubeclient.Interface") {
		t.Error("expected kube kubeclient.Interface field in struct")
	}
	if strings.Contains(src, "informer cache.SharedIndexInformer") {
		t.Error("informer field should not be in struct — use kube.GetInformer()")
	}
	if strings.Contains(src, "ev       event.Recorder") {
		t.Error("ev field should not be in struct — use kube.GetEventRecorder()")
	}
}

func TestRewrite_ConstructorGenerated(t *testing.T) {
	res, err := Rewrite([]byte(baseline), ModeNative)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	src := string(res.Source)

	if !strings.Contains(src, "func NewWebAppReconciler(") {
		t.Error("expected NewWebAppReconciler constructor")
	}
	if !strings.Contains(src, ") domain.Reconciler {") {
		t.Error("expected domain.Reconciler return type on constructor")
	}
}

func TestRewrite_StatusUpdateFlagged(t *testing.T) {
	res, err := Rewrite([]byte(baseline), ModeNative)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	src := string(res.Source)

	if !strings.Contains(src, "TODO(ork migrate)") {
		t.Error("expected TODO(ork migrate) for r.Status().Update()")
	}
	hasWarning := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "PatchStatus") {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Error("expected PatchStatus warning in result.Warnings")
	}
}

func TestRewrite_ReceiverType(t *testing.T) {
	res, err := Rewrite([]byte(baseline), ModeNative)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if res.ReceiverType != "WebAppReconciler" {
		t.Errorf("ReceiverType = %q, want WebAppReconciler", res.ReceiverType)
	}
	if res.PkgName != "controller" {
		t.Errorf("PkgName = %q, want controller", res.PkgName)
	}
}

func TestRewrite_NoCtrlImport(t *testing.T) {
	res, err := Rewrite([]byte(baseline), ModeNative)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	src := string(res.Source)

	if strings.Contains(src, `"sigs.k8s.io/controller-runtime"`) {
		t.Error("ctrl import should be removed")
	}
}

func TestRewrite_RequeueAfterFlagged(t *testing.T) {
	src := `package controller

import (
	"context"
	"time"
	ctrl "sigs.k8s.io/controller-runtime"
)

type MyReconciler struct{}

func (r *MyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}
`
	res, err := Rewrite([]byte(src), ModeNative)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	out := string(res.Source)
	// RequeueAfter is now preserved: ctrl.Result{RequeueAfter: X} → domain.Result{RequeueAfter: X}
	if !strings.Contains(out, "domain.Result{RequeueAfter:") {
		t.Error("expected domain.Result{RequeueAfter: ...} in output")
	}
	if strings.Contains(out, "ctrl.Result") {
		t.Error("ctrl.Result should be replaced")
	}
}

func TestRewrite_ToClientMode_ReconcileUnchanged(t *testing.T) {
	const src = `package controller

import (
	"context"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type WebAppReconciler struct {
	client client.Client
}

func (r *WebAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func (r *WebAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&WebApp{}).Complete(r)
}
`
	res, err := Rewrite([]byte(src), ModeToClient)
	if err != nil {
		t.Fatalf("Rewrite ModeToClient: %v", err)
	}
	out := string(res.Source)

	// Reconcile signature must be untouched.
	if !strings.Contains(out, "req ctrl.Request") {
		t.Error("expected Reconcile signature to be unchanged (req ctrl.Request still present)")
	}
	if strings.Contains(out, "key string") {
		t.Error("expected Reconcile signature NOT to be rewritten to (ctx, key string)")
	}

	// Constructor injected.
	if !strings.Contains(out, "func NewWebAppReconciler(kube kubeclient.Interface)") {
		t.Error("expected ToClient constructor to be injected")
	}
	if !strings.Contains(out, "orkadapter.ToClient(kube)") {
		t.Error("expected orkadapter.ToClient in constructor")
	}
	if !strings.Contains(out, "domain.ReconcilerFrom") {
		t.Error("expected domain.ReconcilerFrom in constructor")
	}

	// SetupWithManager removed.
	if strings.Contains(out, "SetupWithManager") && !strings.Contains(out, "removed") {
		t.Error("expected SetupWithManager to be removed or replaced with comment")
	}

	// ctrl import must be kept — signature and body are unchanged.
	if !strings.Contains(out, `"sigs.k8s.io/controller-runtime"`) {
		t.Error("expected ctrl import to be kept in toclient mode")
	}

	// Orkestra imports must be injected (not just TODO comments).
	if !strings.Contains(out, `"github.com/orkspace/orkestra/domain"`) {
		t.Error("expected domain import to be injected")
	}
	if !strings.Contains(out, `"github.com/orkspace/orkestra/pkg/kubeclient"`) {
		t.Error("expected kubeclient import to be injected")
	}
	if !strings.Contains(out, `"github.com/orkspace/orkestra/pkg/kubeclient/orkadapter"`) {
		t.Error("expected orkadapter import to be injected")
	}

	// Mode recorded.
	if res.Mode != ModeToClient {
		t.Errorf("expected Mode ModeToClient, got %q", res.Mode)
	}
}

func TestRewrite_NoReconcileMethod(t *testing.T) {
	src := `package controller

type MyReconciler struct{}

func (r *MyReconciler) DoSomething() {}
`
	_, err := Rewrite([]byte(src), ModeNative)
	if err == nil {
		t.Error("expected error when no Reconcile method found")
	}
}

func TestGenerate_KatalogContainsConstructor(t *testing.T) {
	res := &Result{
		ReceiverType: "WebAppReconciler",
		PkgName:      "controller",
	}
	files := Generate(res, Options{
		ModulePath:   "github.com/example/webapp-operator",
		OperatorName: "webapp-operator",
		OrkVersion:   "v0.8.0",
	})

	if !strings.Contains(files.Katalog, "function: NewWebAppReconciler") {
		t.Error("katalog.yaml should contain constructor function name")
	}
	if !strings.Contains(files.Katalog, "default: false") {
		t.Error("katalog.yaml should have default: false for constructor")
	}
	if !strings.Contains(files.GoMod, "v0.8.0") {
		t.Error("go.mod should contain Orkestra version")
	}
}

func TestExtractPrimaryType(t *testing.T) {
	const src = `package controller

import (
	"context"
	ctrl "sigs.k8s.io/controller-runtime"
	demov1alpha1 "github.com/example/operator/api/v1alpha1"
)

type WebAppReconciler struct{}

func (r *WebAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func (r *WebAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&demov1alpha1.WebApp{}).
		Complete(r)
}
`
	res, err := Rewrite([]byte(src), ModeToClient)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	p := res.Primary
	if p.Kind != "WebApp" {
		t.Errorf("Primary.Kind = %q, want WebApp", p.Kind)
	}
	if p.Object != "WebApp" {
		t.Errorf("Primary.Object = %q, want WebApp", p.Object)
	}
	if p.ObjectList != "WebAppList" {
		t.Errorf("Primary.ObjectList = %q, want WebAppList", p.ObjectList)
	}
	if p.Version != "v1alpha1" {
		t.Errorf("Primary.Version = %q, want v1alpha1", p.Version)
	}
	if p.Location != "github.com/example/operator/api/v1alpha1" {
		t.Errorf("Primary.Location = %q, want github.com/example/operator/api/v1alpha1", p.Location)
	}
	if p.Alias != "demov1alpha1" {
		t.Errorf("Primary.Alias = %q, want demov1alpha1", p.Alias)
	}

	// katalog.yaml should use the detected values
	files := Generate(res, Options{
		ModulePath:   "github.com/example/webapp-operator",
		OperatorName: "webapp-operator",
	})
	if !strings.Contains(files.Katalog, "kind: WebApp") {
		t.Error("katalog should contain kind: WebApp from For()")
	}
	if !strings.Contains(files.Katalog, "object: WebApp") {
		t.Error("katalog should contain object: WebApp")
	}
	if !strings.Contains(files.Katalog, "objectList: WebAppList") {
		t.Error("katalog should contain objectList: WebAppList")
	}
	if !strings.Contains(files.Katalog, "version: v1alpha1") {
		t.Error("katalog should contain version: v1alpha1")
	}
	if !strings.Contains(files.Katalog, "location: github.com/example/operator/api/v1alpha1") {
		t.Error("katalog should contain the detected location")
	}
	if !strings.Contains(files.Katalog, "alias: demov1alpha1") {
		t.Error("katalog should contain alias: demov1alpha1")
	}
}

func TestExtractOwnsWatches(t *testing.T) {
	const src = `package controller

import (
	"context"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	demov1alpha1 "github.com/example/operator/api/v1alpha1"
)

type WebAppReconciler struct{}

func (r *WebAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func (r *WebAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&demov1alpha1.WebApp{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Watches(&demov1alpha1.Config{}, nil).
		Complete(r)
}
`
	res, err := Rewrite([]byte(src), ModeToClient)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	if len(res.Owns) != 2 {
		t.Fatalf("expected 2 Owns entries, got %d: %+v", len(res.Owns), res.Owns)
	}
	ownsKinds := map[string]string{}
	for _, o := range res.Owns {
		ownsKinds[o.Kind] = o.APIVersion
	}
	if ownsKinds["Deployment"] != "apps/v1" {
		t.Errorf("Owns: Deployment APIVersion = %q, want apps/v1", ownsKinds["Deployment"])
	}
	if ownsKinds["Service"] != "v1" {
		t.Errorf("Owns: Service APIVersion = %q, want v1", ownsKinds["Service"])
	}

	if len(res.Watches) != 1 {
		t.Fatalf("expected 1 Watches entry, got %d: %+v", len(res.Watches), res.Watches)
	}
	if res.Watches[0].Kind != "Config" {
		t.Errorf("Watches[0].Kind = %q, want Config", res.Watches[0].Kind)
	}
	if !strings.Contains(res.Watches[0].APIVersion, "TODO") {
		t.Errorf("Watches[0].APIVersion = %q, want TODO (custom package)", res.Watches[0].APIVersion)
	}
}

func TestGenerate_KatalogWithOwnsWatches(t *testing.T) {
	res := &Result{
		ReceiverType: "WebAppReconciler",
		PkgName:      "controller",
		Owns: []DetectedType{
			{Kind: "Deployment", APIVersion: "apps/v1"},
			{Kind: "Service", APIVersion: "v1"},
		},
		Watches: []DetectedType{
			{Kind: "Config", APIVersion: "TODO: github.com/example/operator/api/v1alpha1"},
		},
	}
	files := Generate(res, Options{
		ModulePath:   "github.com/example/webapp-operator",
		OperatorName: "webapp-operator",
	})

	if !strings.Contains(files.Katalog, "kind: Deployment") {
		t.Error("katalog should contain Deployment from Owns()")
	}
	if !strings.Contains(files.Katalog, "kind: Service") {
		t.Error("katalog should contain Service from Owns()")
	}
	if !strings.Contains(files.Katalog, "watch:") {
		t.Error("katalog should contain watch: block from Watches()")
	}
	if !strings.Contains(files.Katalog, "kind: Config") {
		t.Error("katalog should contain Config in watch: block")
	}
}

func TestToKebab(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"WebAppReconciler", "web-app-reconciler"},
		{"DatabaseOperator", "database-operator"},
		{"MyReconciler", "my-reconciler"},
		{"", "my-operator"},
	}
	for _, c := range cases {
		got := toKebab(c.in)
		if got != c.want {
			t.Errorf("toKebab(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
