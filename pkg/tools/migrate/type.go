package migrate

const (
	domain     = "github.com/orkspace/orkestra/domain"
	kubeclient = "github.com/orkspace/orkestra/pkg/kubeclient"
	ctrlclient = "github.com/orkspace/orkestra/pkg/kubeclient/orkadapter"
)

// Mode controls how much of the source file migrate rewrites.
type Mode string

const (
	// ModeNative rewrites the full controller-runtime signature to Orkestra's
	// native style: Reconcile(ctx context.Context, req domain.Request) (domain.Result, error),
	// struct fields replaced, call sites adapted. Most invasive; produces fully idiomatic Orkestra code.
	ModeNative Mode = "native"

	// ModeToClient is the minimal migration path. The Reconcile signature,
	// struct fields, and call sites are left completely unchanged. Only
	// SetupWithManager is removed and a constructor using orkadapter.ToClient
	// and domain.ReconcilerFrom is injected. Two lines of new code; zero
	// changes to existing reconciler logic.
	ModeToClient Mode = "toclient"
)

// Result holds the output of a migration rewrite.
type Result struct {
	// Source is the rewritten Go source, gofmt-formatted.
	Source []byte
	// ReceiverType is the struct name from the Reconcile receiver (e.g. "WebAppReconciler").
	ReceiverType string
	// PkgName is the Go package name of the source file.
	PkgName string
	// Warnings are patterns flagged but not automatically rewritten.
	Warnings []string
	// Mode is the migration mode used to produce this result.
	Mode Mode
	// Owns lists types detected in Owns() calls inside SetupWithManager.
	// Each entry is a resource the operator owns and should appear in constructor.managedResources:.
	Owns []DetectedType
	// Watches lists types detected in Watches() calls inside SetupWithManager.
	// Each entry should appear in operatorBox.observe.watch:.
	Watches []DetectedType
	// Primary holds the type information extracted from the For() call in SetupWithManager.
	Primary PrimaryType
}

// DetectedType is a resource type extracted from an Owns() or Watches() call.
type DetectedType struct {
	Kind       string
	APIVersion string // best-effort from import path; may be TODO if not resolvable
}

// PrimaryType holds the type information extracted from the For() call in SetupWithManager.
// All fields are best-effort; unresolvable fields are left empty.
type PrimaryType struct {
	Kind       string // struct name from For(&pkg.Kind{})
	Object     string // same as Kind
	ObjectList string // Kind + "List" by convention
	Version    string // last path segment of import when it looks like a version (e.g. v1alpha1)
	Location   string // full import path of the package (e.g. github.com/org/project/api/v1alpha1)
	Alias      string // import alias used in source (e.g. demov1alpha1)
}

// replacement is a byte-range substitution to apply to source text.
type replacement struct {
	start int
	end   int
	text  string
}

// importInfo holds the resolved values for one import declaration.
type importInfo struct {
	Path       string // full import path
	APIVersion string // best-effort Kubernetes apiVersion
}
