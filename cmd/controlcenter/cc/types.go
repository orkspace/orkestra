package controlcenter

// KatalogListData is the data for the main index page
type KatalogListData struct {
	Katalogs        []KatalogSummary
	TotalKatalogs   int
	HealthyKatalogs int
	TotalCRDs       int
	TotalWorkers    int
	TotalResources  int
	AnyHealthy      bool
	OrkestraURLs    string
}

// KatalogSummary is a summary of a Katalog for the list view
type KatalogSummary struct {
	Name             string
	Description      string
	Version          string
	Healthy          bool
	CreatedBy        string
	AppCount         int
	TotalCRDs        int
	HealthyCRDs      int
	TotalWorkers     int
	TotalResources   int
	ClusterName      string
	Namespaces       []string                           // distinct namespace values from this Katalog's CRDs
	NamespaceDetails map[string]KatalogNamespaceSummary // per-namespace stats from the runtime
}

// KatalogData is the data for the Katalog dashboard view
type KatalogData struct {
	CRDs               []CRDSummary
	TotalCRDs          int
	OrkReady           bool
	DeletionProtection bool
	TotalWorkers       int
	TotalResources     int
	HealthyCount       int
	KatalogName        string
	KatalogDescription string
	KatalogHealthy     bool
	KatalogVersion     string
	KatalogAuthor      string
	KatalogLicense     string
	DegradedReason     string
	StatusCounts       StatusCounts
	RuntimeVersion     string
	ClusterName        string
}

// IndexData is the data for the main page
type IndexData struct {
	Katalogs             []KatalogSummary
	TotalKatalogs        int
	HealthyKatalogs      int
	TotalApps            int
	TotalCRDs            int
	TotalWorkers         int
	TotalResources       int
	AnyHealthy           bool
	HasOperatorKatalogs  bool
	OrkestraURLs         string
	EnableRuntimeManager bool
	CCVersion            string
	AllClusters          []string // distinct cluster names for filter checkboxes
	AllNamespaces        []string // distinct namespace names for filter checkboxes
}

// StatusCounts tracks CRD health counts
type StatusCounts struct {
	Healthy  int `json:"healthy"`
	Degraded int `json:"degraded"`
	Started  int `json:"started"`
	Pending  int `json:"pending"`
	Gated    int `json:"gated"`
}

// RBACInfo is the data for the main page
type RBACInfo struct {
	Rules      []RBACRule `json:"rules"`
	Summary    string     `json:"summary"`
	TotalRules int        `json:"totalRules"`
}

type RBACRule struct {
	APIGroups []string `json:"apiGroups,omitempty"`
	Resources []string `json:"resources,omitempty"`
	Verbs     []string `json:"verbs,omitempty"`
	// Human-readable description
	Description string `json:"description,omitempty"`
}

// KatalogNamespaceSummary mirrors the runtime's namespace grouping.
type KatalogNamespaceSummary struct {
	CRDs         []string     `json:"crds"`
	StatusCounts StatusCounts `json:"statusCounts"`
	Healthy      bool         `json:"healthy"`
	Description  string       `json:"description,omitempty"`
	Version      string       `json:"version,omitempty"`
	Workers      int          `json:"workers"`
	Resources    int          `json:"resources"`
}

// KatalogResponse is the response from the /katalog endpoint
type KatalogResponse struct {
	Total              int                                `json:"total"`
	TotalEnabled       int                                `json:"totalEnabled"`
	Healthy            bool                               `json:"healthy"`
	Status             int                                `json:"status"`
	OrkReady           bool                               `json:"OrkReady"`
	IsKonductor        bool                               `json:"isKonductor"`
	DeletionProtection bool                               `json:"deletionProtection"`
	CRDs               []CRDSummary                       `json:"crds"`
	Name               string                             `json:"name,omitempty"`
	Version            string                             `json:"version,omitempty"`
	Author             string                             `json:"author,omitempty"`
	Description        string                             `json:"description,omitempty"`
	DegradedReason     string                             `json:"degradedReason,omitempty"`
	StatusCounts       StatusCounts                       `json:"statusCounts"`
	License            string                             `json:"license,omitempty"`
	RuntimeVersion     string                             `json:"runtimeVersion,omitempty"`
	ClusterName        string                             `json:"clusterName,omitempty"`
	CreatedBy          string                             `json:"createdBy,omitempty"`
	Projects           map[string]ProjectInfoSummary      `json:"projects,omitempty"`
	Namespaces         map[string]KatalogNamespaceSummary `json:"namespaces,omitempty"`
	GatewayEndpoint    string                             `json:"gatewayEndpoint,omitempty"`
	IdpEnabled         bool                               `json:"idpEnabled,omitempty"`
}

// GatewayKatalogResponse mirrors the response served at GET /katalog by the
// gateway process. Used by the control center to merge per-CRD webhook stats
// with the runtime's reconciler stats by GVR key.
type GatewayKatalogResponse struct {
	Source                     string            `json:"source"`
	Name                       string            `json:"name"`
	Version                    string            `json:"version,omitempty"`
	AdmissionEnabled           bool              `json:"admissionEnabled"`
	ConversionEnabled          bool              `json:"conversionEnabled"`
	DeletionProtectionEnabled  bool              `json:"deletionProtectionEnabled"`
	NamespaceProtectionEnabled bool              `json:"namespaceProtectionEnabled"`
	CRDs                       []GatewayCRDStats `json:"crds"`
	GatewayVersion             string            `json:"gatewayVersion,omitempty"`
}

// GatewayCRDStats holds the gateway-owned stats for one CRD.
// GVR is the merge key: "group/version/resource".
type GatewayCRDStats struct {
	Name                string                    `json:"name"`
	GVK                 string                    `json:"gvk"`
	GVR                 string                    `json:"gvr"`
	Admission           *AdmissionStats           `json:"admission,omitempty"`
	Conversion          *ConversionStats          `json:"conversion,omitempty"`
	DeletionProtection  *DeletionProtectionStats  `json:"deletionProtection,omitempty"`
	NamespaceProtection *NamespaceProtectionStats `json:"namespaceProtection,omitempty"`
}

// ProjectInfoSummary is the CC-side view of one app in KatalogResponse.Projects.
// Fields mirror interface{} — add here when the runtime starts sending more.
type ProjectInfoSummary struct {
	Name          string `json:"name"`
	Namespace     string `json:"namespace"`
	Port          string `json:"port,omitempty"`
	Language      string `json:"language,omitempty"`
	CurrentImage  string `json:"currentImage,omitempty"`
	GitCommit     string `json:"gitCommit,omitempty"`
	License       string `json:"license,omitempty"`
	HasDockerfile bool   `json:"hasDockerfile,omitempty"`
	HasFrontend   bool   `json:"hasFrontend,omitempty"`
	HasSMTP       bool   `json:"hasSMTP,omitempty"`
	HasSlack      bool   `json:"hasSlack,omitempty"`
	HasCompose    bool   `json:"hasCompose,omitempty"`
	SecretCount   int    `json:"secretCount,omitempty"`
	ConfigCount   int    `json:"configCount,omitempty"`
}

// DevAppsData is passed to the developer-view template.
type DevAppsData struct {
	KatalogName    string
	Apps           []DevAppSummary
	CCVersion      string
	RuntimeVersion string
}

// DevAppSummary holds display data for one app in the developer view.
type DevAppSummary struct {
	Name          string
	Namespace     string
	Port          string
	Language      string
	CurrentImage  string
	ImageTag      string
	ServiceURL    string
	GitCommit     string
	License       string
	HasDockerfile bool
	HasFrontend   bool
	HasSMTP       bool
	HasSlack      bool
	HasCompose    bool
	SecretCount   int
	ConfigCount   int
}

// DevAppDetailData is passed to the app detail template.
type DevAppDetailData struct {
	KatalogName    string
	App            DevAppSummary
	RuntimeVersion string
}

type EndpointInfo struct {
	Health        string `json:"health"`
	Info          string `json:"info"`
	HealthEnabled bool   `json:"healthEnabled"`
	InfoEnabled   bool   `json:"infoEnabled"`
}

// CRDSummary is a summary of a CRD
type CRDSummary struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Mode        string `json:"mode,omitempty"`
	GVK         string `json:"gvk,omitempty"`
	GVR         string `json:"gvr,omitempty"`
	// Target is the identifier this CRD is addressed by in the Gateway API and
	// schema API (idp.target, or the lowercased kind when unset). Empty when
	// IDP isn't enabled for this CRD.
	Target                   string       `json:"target,omitempty"`
	Namespaced               bool         `json:"namespaced"`
	Namespace                string       `json:"namespace,omitempty"`
	CrossAccess              bool         `json:"crossAccess"`
	State                    string       `json:"state"` // "healthy", "started", "pending", "degraded"
	Healthy                  bool         `json:"healthy"`
	Started                  bool         `json:"started"`
	Pending                  bool         `json:"pending"`
	Workers                  int          `json:"workers"`
	WorkersActive            int          `json:"workersActive"`
	DependsOn                []string     `json:"dependsOn"`
	WorkersSource            string       `json:"workersSource"`
	QueueDepth               int          `json:"queueDepth"`
	MaxDepth                 int          `json:"maxDepth"`
	ResourceCount            int          `json:"resourceCount"`
	ErrorRate                float64      `json:"errorRate"`
	Uptime                   string       `json:"uptime"`
	RBACCount                int          `json:"rbacCount,omitempty"`
	HasUnhealthyDependencies bool         `json:"hasUnhealthyDependencies"`
	DeletionProtection       bool         `json:"deletionProtection"`
	KatalogNamespace         string       `json:"katalogNamespace,omitempty"`
	Endpoints                EndpointInfo `json:"endpoints,omitempty"`
	IdpEnabled               bool         `json:"idpEnabled,omitempty"`
	RequireServeName         bool         `json:"requireServeName,omitempty"`
	Gated                    bool         `json:"gated,omitempty"`
	GatedReason              string       `json:"gatedReason,omitempty"`
}

// ServeField is one rendered field in the IDP create form.
type ServeField struct {
	Name        string
	Label       string
	InputType   string // "text" | "number" | "select" | "checkbox"
	Placeholder string
	Hint        string
	Enum        []string
	Required    bool
	Category    string // section heading for visual grouping
	WhenJSON    string // JSON array of Condition — all must be true (AND)
	OrJSON      string // JSON array of Condition — at least one must be true (OR)
	Disabled    string // non-empty → greyed-out field with this message
}

// IDPSection is a group of fields sharing a section heading in the IDP form.
type IDPSection struct {
	Title  string
	Fields []ServeField
}

// IDPFormData is the view model for idp_form.html.
type IDPFormData struct {
	KatalogName string
	CRDName     string
	// Target is the identifier submitted to the gateway's Gateway API
	// (idp.target, or the lowercased kind when unset).
	Target string
	// Aliases are the non-primary surfaces for this CRD. When non-empty the
	// form renders a surface dropdown as the first UI element. All surfaces
	// (primary target + aliases) are listed so the user can pick one.
	Aliases []string
	// Kind/APIVersion are display-only — shown in the page header, not used
	// to build the submitted payload (the gateway builds the CR).
	Kind             string
	APIVersion       string
	BackURL          string
	Namespaced       bool
	RequireServeName bool
	Sections         []IDPSection
	Error            string
}

// CRDHealth is the response from the /katalog/{crd}/health endpoint
type CRDHealth struct {
	Name                     string                      `json:"name"`
	State                    string                      `json:"state"`
	IsKonductor              bool                        `json:"isKonductor"`
	Healthy                  bool                        `json:"healthy"`
	Started                  bool                        `json:"started"`
	Pending                  bool                        `json:"pending"`
	StartedAt                string                      `json:"startedAt"`
	Uptime                   string                      `json:"uptime"`
	QueueDepth               int                         `json:"queueDepth"`
	ErrorRate                float64                     `json:"errorRate"`
	ConsecutiveFails         int                         `json:"consecutiveFails"`
	TotalReconciles          int                         `json:"totalReconciles"`
	ResourceCount            int                         `json:"resourceCount"`
	LastError                string                      `json:"lastError"`
	LastReconcile            string                      `json:"lastReconcile"`
	HasUnhealthyDependencies bool                        `json:"hasUnhealthyDependencies"`
	Dependencies             map[string]DependencyStatus `json:"dependencies,omitempty"`
	Gated                    bool                        `json:"gated,omitempty"`
	GatedReason              string                      `json:"gatedReason,omitempty"`
}

type DependencyStatus struct {
	Name                string `json:"name"`
	State               string `json:"state"`
	Condition           string `json:"condition"`
	Satisfied           bool   `json:"satisfied"`
	LastCheck           string `json:"lastCheck,omitempty"`
	AcceptableCondition string `json:"acceptableCondition"` // "started", "healthy", "ready"
}

// AutoscalerWorkersInfo mirrors autoscaler.WorkerInfo from the API response.
type AutoscalerWorkersInfo struct {
	Configured           int     `json:"configured"`
	Effective            int     `json:"effective"`
	InFlight             int     `json:"inFlight"`
	Idle                 int     `json:"idle"`
	Max                  int     `json:"max,omitempty"`
	AutoscalerEnabled    bool    `json:"autoscalerEnabled"`
	OverrideActive       bool    `json:"overrideActive,omitempty"`
	OverrideWorkers      int     `json:"overrideWorkers,omitempty"`
	QueueDepth           int64   `json:"queueDepth"`
	QueueDepthConfigured int     `json:"queueDepthConfigured"`
	QueueDepthEffective  int     `json:"queueDepthEffective"`
	Resync               string  `json:"resync"`
	ResyncEffective      string  `json:"resyncEffective"`
	ResyncConfigured     string  `json:"resyncConfigured"`
	BusyPercent          float64 `json:"busyPercent"`
}

// CRDInfo is the response from the /katalog/{crd} endpoint
type CRDInfo struct {
	Name                     string                    `json:"name"`
	Description              string                    `json:"description"`
	Mode                     string                    `json:"mode"`
	GVK                      string                    `json:"gvk"`
	GVR                      string                    `json:"gvr"`
	Namespaced               bool                      `json:"namespaced"`
	Namespace                string                    `json:"namespace"`
	DependsOn                []string                  `json:"dependsOn"`
	IsKonductor              bool                      `json:"isKonductor"`
	Workers                  int                       `json:"workers"`
	WorkersActive            int32                     `json:"workersActive"`
	WorkersIdle              int32                     `json:"workersIdle"`
	WorkersProcessing        int32                     `json:"workersProcessing"`
	WorkerDetails            map[string]string         `json:"workerDetails,omitempty"`
	WorkersSource            string                    `json:"workersSource"`
	Resync                   string                    `json:"resync"`
	ResyncSource             string                    `json:"resyncSource"`
	QueueDepth               int                       `json:"queueDepth"`
	MaxDepth                 int                       `json:"maxDepth"`
	MaxDepthSource           string                    `json:"maxDepthSource"`
	ResourceCount            int                       `json:"resourceCount"`
	TotalReconciles          int                       `json:"totalReconciles"`
	OperatorBox              map[string]interface{}    `json:"operatorBox"`
	Healthy                  bool                      `json:"healthy"`
	Started                  bool                      `json:"started"`
	ErrorRate                float64                   `json:"errorRate"`
	Conversion               *ConversionStats          `json:"conversion"`
	Admission                *AdmissionStats           `json:"admission"`
	DeletionProtection       *DeletionProtectionStats  `json:"deletionProtection,omitempty"`
	NamespaceProtection      *NamespaceProtectionStats `json:"namespaceProtection,omitempty"`
	RBAC                     RBACInfo                  `json:"rbac,omitempty"`
	HasUnhealthyDependencies bool                      `json:"hasUnhealthyDependencies"`
	AutoscalerEnabled        bool                      `json:"autoscalerEnabled"`
	AutoscalerWorkers        *AutoscalerWorkersInfo    `json:"autoscalerWorkers,omitempty"`
	Gated                    bool                      `json:"gated,omitempty"`
	GatedReason              string                    `json:"gatedReason,omitempty"`
}

// ConversionStats contains version conversion metrics
type ConversionStats struct {
	Enabled      bool    `json:"enabled"`
	Total        int     `json:"total"`
	Success      int     `json:"success"`
	Failures     int     `json:"failures"`
	AvgLatencyMs float64 `json:"avgLatencyMs"`
	P95LatencyMs float64 `json:"p95LatencyMs"`
}

// DeletionProtectionStats contains deletion protection status
type DeletionProtectionStats struct {
	Enabled bool `json:"enabled"`
	Total   int  `json:"total"`
	Blocked int  `json:"blocked"`
	Allowed int  `json:"allowed"`
}

// NamespaceProtectionStats contains namespace protection status and the declared namespace rules.
type NamespaceProtectionStats struct {
	Enabled              bool     `json:"enabled"`
	HasNamespaceRules    bool     `json:"hasNamespaceRules"`
	Total                int      `json:"total"`
	Blocked              int      `json:"blocked"`
	Allowed              int      `json:"allowed"`
	AllowedNamespaces    []string `json:"allowedNamespaces,omitempty"`
	RestrictedNamespaces []string `json:"restrictedNamespaces,omitempty"`
}

// AdmissionStats contains admission webhook metrics
type AdmissionStats struct {
	WebhooksEnabled   bool    `json:"webhooksEnabled"`
	ValidationTotal   int     `json:"validationTotal"`
	ValidationAllowed int     `json:"validationAllowed"`
	ValidationDenied  int     `json:"validationDenied"`
	ValidationWarned  int     `json:"validationWarned"`
	ValAvgLatencyMs   float64 `json:"valAvgLatencyMs"`
	ValP95LatencyMs   float64 `json:"valP95LatencyMs"`
	ValMaxLatencyMs   float64 `json:"valMaxLatencyMs"`
	MutationTotal     int     `json:"mutationTotal"`
	MutationApplied   int     `json:"mutationApplied"`
	MutationSkipped   int     `json:"mutationSkipped"`
	MutAvgLatencyMs   float64 `json:"mutAvgLatencyMs"`
	MutP95LatencyMs   float64 `json:"mutP95LatencyMs"`
	MutMaxLatencyMs   float64 `json:"mutMaxLatencyMs"`
}

// CRDDetail combines health and info for a single CRD
type CRDDetail struct {
	State                    string                      `json:"state"`
	StartedAt                string                      `json:"startedAt"`
	Uptime                   string                      `json:"uptime"`
	ConsecutiveFails         int                         `json:"consecutiveFails"`
	LastError                string                      `json:"lastError"`
	LastReconcile            string                      `json:"lastReconcile"`
	StartedAgo               string                      `json:"startedAgo"`
	LastReconcileAgo         string                      `json:"lastReconcileAgo"`
	Name                     string                      `json:"name"`
	Description              string                      `json:"description"`
	Mode                     string                      `json:"mode"`
	GVK                      string                      `json:"gvk"`
	GVR                      string                      `json:"gvr"`
	Namespaced               bool                        `json:"namespaced"`
	Namespace                string                      `json:"namespace"`
	CrossAccess              bool                        `json:"crossAccess"`
	DependsOn                []string                    `json:"dependsOn"`
	HasUnhealthyDependencies bool                        `json:"hasUnhealthyDependencies"`
	Dependencies             map[string]DependencyStatus `json:"dependencies,omitempty"`
	Workers                  int                         `json:"workers"`
	WorkersActive            int                         `json:"workersActive"`
	WorkersIdle              int                         `json:"workersIdle"`
	WorkersProcessing        int                         `json:"workersProcessing"`
	WorkerDetails            map[string]string           `json:"workerDetails,omitempty"`
	WorkersSource            string                      `json:"workersSource"`
	Resync                   string                      `json:"resync"`
	ResyncSource             string                      `json:"resyncSource"`
	QueueDepth               int                         `json:"queueDepth"`
	MaxDepth                 int                         `json:"maxDepth"`
	MaxDepthSource           string                      `json:"maxDepthSource"`
	ResourceCount            int                         `json:"resourceCount"`
	TotalReconciles          int                         `json:"totalReconciles"`
	OperatorBox              map[string]interface{}      `json:"operatorBox"`
	Healthy                  bool                        `json:"healthy"`
	Started                  bool                        `json:"started"`
	Pending                  bool                        `json:"pending"`
	ErrorRate                float64                     `json:"errorRate"`
	Conversion               *ConversionStats            `json:"conversion"`
	Admission                *AdmissionStats             `json:"admission"`
	DeletionProtection       *DeletionProtectionStats    `json:"deletionProtection,omitempty"`
	NamespaceProtection      *NamespaceProtectionStats   `json:"namespaceProtection,omitempty"`
	RBAC                     RBACInfo                    `json:"rbac,omitempty"`
	RBACCount                int                         `json:"rbacCount,omitempty"`
	AutoscalerEnabled        bool                        `json:"autoscalerEnabled"`
	AutoscalerWorkers        *AutoscalerWorkersInfo      `json:"autoscalerWorkers,omitempty"`
	HealthEndpointDisabled   bool                        `json:"healthEndpointDisabled,omitempty"`
	InfoEndpointDisabled     bool                        `json:"infoEndpointDisabled,omitempty"`
	Gated                    bool                        `json:"gated,omitempty"`
	GatedReason              string                      `json:"gatedReason,omitempty"`
}

// TODO: Future
type SimpleSystemMetrics struct {
	Goroutines int     `json:"goroutines"`
	Threads    int     `json:"threads"`
	Gomaxprocs int     `json:"gomaxprocs"`
	HeapAlloc  uint64  `json:"heapAlloc"`
	HeapSys    uint64  `json:"heapSys"`
	GCPauseAvg float64 `json:"gcPauseAvg"`
}
