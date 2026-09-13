// pkg/runtime/kordinator/vitals/cr_handlers.go
//
// CR-level endpoints — list, detail, and events per CRD instance.
//
// These endpoints extend the existing /katalog/{crd} surface with:
//
//   GET /katalog/{crd}/cr
//       List all CR instances for a CRD — the kubectl get equivalent.
//       Reads from the informer cache — no API server calls.
//       Returns name, namespace, phase, ready, age, generation.
//       ?field=<dot-path> additionally resolves that field (e.g. spec.domain)
//       against each instance's full object and returns it as "value" —
//       used by the gateway's operator: unique admission check.
//
//   GET /katalog/{crd}/cr/{name}           (cluster-scoped CRDs)
//   GET /katalog/{crd}/cr/{namespace}/{name} (namespaced CRDs)
//       Full CR detail: status fields, conditions, children.
//       Children are read on demand from the API server.
//
//   GET /katalog/{crd}/cr/{name}/events
//   GET /katalog/{crd}/cr/{namespace}/{name}/events
//       Recent Kubernetes events for this CR — both emitted by Orkestra
//       (Reconciled, ReconcileError) and by Kubernetes itself.
//
// Registration — add to wherever BuildKatalogHandler is registered:
//
//   crHandler := crdhealth.NewCRHandler(kube, reg, rcMap)
//   mux.Handle("/katalog/", crHandler.Route(existingKatalogMux))
//
// Or call BuildCRListHandler / BuildCRDetailHandler / BuildCREventsHandler
// directly alongside the existing per-CRD handler registrations.

package vitals

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/orkspace/orkestra/pkg/kubeclient"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/cache"
)

const (
	listOptionsLimit = 1
	fetchTimeout     = 3 * time.Second
)

// ─────────────────────────────────────────────────────────────────────────────
// Response types
// ─────────────────────────────────────────────────────────────────────────────

// CRSummary is one row in the CR list — equivalent to one line of kubectl get.
type CRSummary struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace,omitempty"`
	Phase       string `json:"phase,omitempty"` // from status.phase if declared
	Ready       bool   `json:"ready"`           // from status.conditions[type=Ready]
	ReadyReason string `json:"readyReason,omitempty"`
	Age         string `json:"age"`
	Generation  int64  `json:"generation"`

	// Value is the resolved value of the ?field= dot-path requested on this
	// list call — e.g. ?field=spec.domain. Empty when no field was
	// requested, or when the CR doesn't have that field. This is what lets
	// operator: unique check other instances without a live List() call
	// against the API server: the informer cache already holds full spec
	// data, this just surfaces one field of it instead of discarding
	// everything but the summary. See pkg/utils/common/query.
	Value string `json:"value,omitempty"`
}

// CRListResponse is returned by GET /katalog/{crd}/cr.
type CRListResponse struct {
	CRD         string      `json:"crd"`
	GVK         string      `json:"gvk"`
	Total       int         `json:"total"`
	Items       []CRSummary `json:"items"`
	IsKonductor bool        `json:"isKonductor"`
}

// ChildSummary is a condensed view of one child resource.
// Only the fields that matter for observability are included — not the full object.
type ChildSummary struct {
	Name      string                 `json:"name"`
	Namespace string                 `json:"namespace,omitempty"`
	Kind      string                 `json:"kind"`
	Status    map[string]interface{} `json:"status,omitempty"`
	Ready     bool                   `json:"ready"`
}

// CRDetailResponse is returned by GET /katalog/{crd}/cr/{namespace}/{name}.
type CRDetailResponse struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace,omitempty"`
	Generation        int64             `json:"generation"`
	CreationTimestamp string            `json:"creationTimestamp"`
	Labels            map[string]string `json:"labels,omitempty"`
	Annotations       map[string]string `json:"annotations,omitempty"`

	Ready        bool   `json:"ready"`
	ReadyReason  string `json:"readyReason,omitempty"`
	ReadyMessage string `json:"readyMessage,omitempty"`

	Status   map[string]interface{} `json:"status,omitempty"`
	Children map[string]interface{} `json:"children"`

	EventsEndpoint string `json:"eventsEndpoint"`
	IsKonductor    bool   `json:"isKonductor"`
}

// CREvent is one Kubernetes event involving this CR or its children.
type CREvent struct {
	Type      string `json:"type"` // Normal or Warning
	Reason    string `json:"reason"`
	Message   string `json:"message"`
	Source    string `json:"source"` // component that emitted the event
	Object    string `json:"object"` // involved object kind/name
	Count     int32  `json:"count"`
	FirstSeen string `json:"firstSeen"`
	LastSeen  string `json:"lastSeen"`
	Age       string `json:"age"`
}

// CREventsResponse is returned by GET /katalog/{crd}/cr/{namespace}/{name}/events.
type CREventsResponse struct {
	Name      string    `json:"name"`
	Namespace string    `json:"namespace,omitempty"`
	Total     int       `json:"total"`
	Events    []CREvent `json:"events"`
}

// ─────────────────────────────────────────────────────────────────────────────
// CR List Handler
// GET /katalog/{crd}/cr
//
// Lists all CR instances from the informer cache.
// No API server calls — purely in-memory.
// Sort order: namespace/name ascending for deterministic output.
// ─────────────────────────────────────────────────────────────────────────────
func BuildCRListHandler(
	crd orktypes.CRDEntry,
	inf cache.SharedIndexInformer,
	o *RuntimeHealth,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if inf == nil {
			utils.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "informer not ready — CRD may still be starting",
			})
			return
		}

		field := r.URL.Query().Get("field")

		objs := inf.GetIndexer().List()
		items := make([]CRSummary, 0, len(objs))

		for _, raw := range objs {
			objMap, err := rawToMap(raw)
			if err != nil {
				continue
			}
			item := summariseCR(objMap, crd.APITypes.Kind)
			if field != "" {
				item.Value, _ = orktypes.ResolveScalarField(objMap, field)
			}
			items = append(items, item)
		}

		sort.Slice(items, func(i, j int) bool {
			ki := items[i].Namespace + "/" + items[i].Name
			kj := items[j].Namespace + "/" + items[j].Name
			return ki < kj
		})

		utils.WriteJSON(w, http.StatusOK, CRListResponse{
			CRD:         crd.Name,
			GVK:         crd.GVKString(),
			Total:       len(items),
			Items:       items,
			IsKonductor: o.IsKonductor(),
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CR Detail Handler
// GET /katalog/{crd}/cr/{name}              (cluster-scoped)
// GET /katalog/{crd}/cr/{namespace}/{name}  (namespaced)
//
// Returns the full CR status and its child resources.
// CR is read from the informer cache — no API server call.
// Children are read from the API server on demand.
//
// rcMap maps GVK → OperatorBoxConfig so we know which resource types
// to look for as children (from onCreate/onReconcile templates).
// ─────────────────────────────────────────────────────────────────────────────
func BuildCRDetailHandler(
	crd orktypes.CRDEntry,
	inf cache.SharedIndexInformer,
	kube *kubeclient.Kubeclient,
	rc orktypes.OperatorBoxConfig,
	o *RuntimeHealth,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if inf == nil {
			utils.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "informer not ready",
			})
			return
		}

		suffix := strings.TrimPrefix(r.URL.Path, "/")
		parts := strings.SplitN(suffix, "/", 2)

		var namespace, name string
		switch len(parts) {
		case 1:
			name = parts[0]
		case 2:
			namespace = parts[0]
			name = parts[1]
		default:
			http.NotFound(w, r)
			return
		}

		key := name
		if namespace != "" {
			key = namespace + "/" + name
		}
		raw, exists, err := inf.GetIndexer().GetByKey(key)
		if err != nil || !exists {
			utils.WriteJSON(w, http.StatusNotFound, map[string]string{
				"error": fmt.Sprintf("CR %q not found", key),
			})
			return
		}

		objMap, err := rawToMap(raw)
		if err != nil {
			utils.WriteJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "unexpected object type in cache",
			})
			return
		}

		children := readChildrenForEndpoint(r.Context(), kube, objMap, rc)
		eventsPath := buildEventsPath(crd, namespace, name)

		detail := buildCRDetail(objMap, children, eventsPath, crd.APITypes.Kind)
		detail.IsKonductor = o.IsKonductor()
		utils.WriteJSON(w, http.StatusOK, detail)
	}
}

// BuildCRDetailAndEventsHandler handles all sub-paths under /katalog/{crd}/cr/
// Dispatches to detail or events based on whether the path ends with /events.
// Register at "/katalog/{crd}/cr/" (with trailing slash).
func BuildCRDetailAndEventsHandler(
	crd orktypes.CRDEntry,
	inf cache.SharedIndexInformer,
	kube *kubeclient.Kubeclient,
	rc orktypes.OperatorBoxConfig,
	o *RuntimeHealth,
) http.HandlerFunc {
	detail := BuildCRDetailHandler(crd, inf, kube, rc, o)
	events := BuildCREventsHandler(crd, kube)

	return func(w http.ResponseWriter, r *http.Request) {
		prefix := "/katalog/" + strings.ToLower(crd.Name) + "/cr/"
		suffix := strings.TrimPrefix(r.URL.Path, prefix)

		if strings.HasSuffix(suffix, "/events") {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/" + strings.TrimSuffix(suffix, "/events")
			events.ServeHTTP(w, r2)
			return
		}

		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + suffix
		detail.ServeHTTP(w, r2)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CR Events Handler
// GET /katalog/{crd}/cr/{name}/events              (cluster-scoped)
// GET /katalog/{crd}/cr/{namespace}/{name}/events  (namespaced)
//
// Returns recent Kubernetes events involving this CR.
// Events are listed from the API server using field selectors on
// involvedObject.name and involvedObject.namespace.
//
// Includes events emitted by Orkestra (reason: Reconciled, ReconcileError,
// ValidationWarning, Deleting) and by Kubernetes itself.
//
// Events are sorted newest-first and capped at 100.
// ─────────────────────────────────────────────────────────────────────────────
func BuildCREventsHandler(
	crd orktypes.CRDEntry,
	kube *kubeclient.Kubeclient,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Parse namespace and name from URL suffix (after /events stripped by mux)
		suffix := strings.TrimSuffix(
			strings.TrimPrefix(r.URL.Path, "/"),
			"/events",
		)
		parts := strings.SplitN(suffix, "/", 2)

		var namespace, name string
		switch len(parts) {
		case 1:
			name = parts[0]
		case 2:
			namespace = parts[0]
			name = parts[1]
		default:
			http.NotFound(w, r)
			return
		}

		events, err := fetchCREvents(r.Context(), kube, namespace, name, crd.APITypes.Kind)
		if err != nil {
			utils.WriteJSON(w, http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("listing events: %v", err),
			})
			return
		}

		utils.WriteJSON(w, http.StatusOK, CREventsResponse{
			Name:      name,
			Namespace: namespace,
			Total:     len(events),
			Events:    events,
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────────────────────────────────────

// summariseCR extracts the summary fields from one CR object.
func summariseCR(objMap map[string]interface{}, parentKind string) CRSummary {
	status, _ := objMap["status"].(map[string]interface{})
	phase, _ := status["phase"].(string)
	ready, readyReason, _ := extractParentReady(objMap, parentKind)
	ts := metaField(objMap, "creationTimestamp")
	var age string
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		age = formatAge(t)
	}
	meta, _ := objMap["metadata"].(map[string]interface{})
	var generation int64
	if g, ok := meta["generation"].(float64); ok {
		generation = int64(g)
	}

	return CRSummary{
		Name:        metaField(objMap, "name"),
		Namespace:   metaField(objMap, "namespace"),
		Phase:       phase,
		Ready:       ready,
		ReadyReason: readyReason,
		Age:         age,
		Generation:  generation,
	}
}

// buildCRDetail assembles the full detail response for one CR.
func buildCRDetail(objMap map[string]interface{}, children map[string]interface{}, eventsEndpoint string, parentKind string) CRDetailResponse {
	ready, readyReason, readyMsg := extractParentReady(objMap, parentKind)

	status, _ := objMap["status"].(map[string]interface{})
	meta, _ := objMap["metadata"].(map[string]interface{})

	// Filter Orkestra system annotations from the public response
	rawAnnotations, _ := meta["annotations"].(map[string]interface{})
	annotations := make(map[string]string, len(rawAnnotations))
	for k, v := range rawAnnotations {
		s, _ := v.(string)
		if !strings.HasPrefix(k, "orkestra.orkspace.io/") &&
			k != "kubectl.kubernetes.io/last-applied-configuration" {
			annotations[k] = s
		}
	}

	rawLabels, _ := meta["labels"].(map[string]interface{})
	labels := make(map[string]string, len(rawLabels))
	for k, v := range rawLabels {
		labels[k], _ = v.(string)
	}

	ts := metaField(objMap, "creationTimestamp")
	var creationTimestamp string
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		creationTimestamp = t.UTC().Format(time.RFC3339)
	}

	var generation int64
	if g, ok := meta["generation"].(float64); ok {
		generation = int64(g)
	}

	return CRDetailResponse{
		Name:              metaField(objMap, "name"),
		Namespace:         metaField(objMap, "namespace"),
		Generation:        generation,
		CreationTimestamp: creationTimestamp,
		Labels:            labels,
		Annotations:       annotations,
		Ready:             ready,
		ReadyReason:       readyReason,
		ReadyMessage:      readyMsg,
		Status:            status,
		Children:          children,
		EventsEndpoint:    eventsEndpoint,
	}
}

// rawToMap and metaField delegate to pkg/utils — single canonical implementation.
func rawToMap(raw interface{}) (map[string]interface{}, error) {
	return utils.RawToMap(raw)
}

func metaField(objMap map[string]interface{}, field string) string {
	return utils.MetaField(objMap, field)
}

// fetchCREvents lists Kubernetes events for a specific CR by name.
// Uses field selectors — no label selector needed since events reference
// their involved object directly.
// Capped at a 3-second deadline — events are supplementary information.
func fetchCREvents(
	ctx context.Context,
	kube *kubeclient.Kubeclient,
	namespace, name, kind string,
) ([]CREvent, error) {
	// Hard deadline — events should never block the page render
	evCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	if namespace == "" {
		namespace = metav1.NamespaceAll
	}

	// Field selector: events where involvedObject matches this CR
	selector := fields.AndSelectors(
		fields.OneTermEqualSelector("involvedObject.name", name),
		fields.OneTermEqualSelector("involvedObject.kind", kind),
	)
	if namespace != metav1.NamespaceAll {
		selector = fields.AndSelectors(
			selector,
			fields.OneTermEqualSelector("involvedObject.namespace", namespace),
		)
	}

	eventList, err := kube.Clientset().CoreV1().Events(namespace).List(evCtx, metav1.ListOptions{
		FieldSelector:   selector.String(),
		Limit:           100,
		ResourceVersion: "0", // watch cache — avoids etcd round-trip
	})
	if err != nil {
		return nil, err
	}

	events := make([]CREvent, 0, len(eventList.Items))
	for _, ev := range eventList.Items {
		events = append(events, summariseEvent(ev))
	}

	// Sort newest-first — most recent event is what operators care about
	sort.Slice(events, func(i, j int) bool {
		return events[i].LastSeen > events[j].LastSeen
	})

	return events, nil
}

// summariseEvent converts a Kubernetes Event to the API response shape.
func summariseEvent(ev corev1.Event) CREvent {
	firstSeen := ev.FirstTimestamp.UTC().Format(time.RFC3339)
	lastSeen := ev.LastTimestamp.UTC().Format(time.RFC3339)

	// Use EventTime for newer events that use the eventTime field instead
	if !ev.EventTime.IsZero() {
		lastSeen = ev.EventTime.UTC().Format(time.RFC3339)
	}

	return CREvent{
		Type:      ev.Type,
		Reason:    ev.Reason,
		Message:   ev.Message,
		Source:    ev.Source.Component,
		Object:    fmt.Sprintf("%s/%s", ev.InvolvedObject.Kind, ev.InvolvedObject.Name),
		Count:     ev.Count,
		FirstSeen: firstSeen,
		LastSeen:  lastSeen,
		Age:       formatAge(ev.LastTimestamp.Time),
	}
}

// buildEventsPath constructs the events endpoint URL for a CR.
func buildEventsPath(crd orktypes.CRDEntry, namespace, name string) string {
	base := "/katalog/" + strings.ToLower(crd.Name) + "/cr"
	if namespace != "" {
		return fmt.Sprintf("%s/%s/%s/events", base, namespace, name)
	}
	return fmt.Sprintf("%s/%s/events", base, name)
}

// formatAge returns a human-readable age string from a time.Time.
func formatAge(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t).Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
