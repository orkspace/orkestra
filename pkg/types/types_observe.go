package types

import (
	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ── Observe event types ─────────────────────────────────────────────────────

// ObserveEvent is the string type for observe event types used in Observe.On.
type ObserveEvent string

const (
	ObserveEventCreate ObserveEvent = "create"
	ObserveEventUpdate ObserveEvent = "update"
	ObserveEventDelete ObserveEvent = "delete"
)

// String() stringifies an observe event
func (o ObserveEvent) String() string {
	return string(o)
}

// observeOn reports whether the given event matches any entry in on.
// An empty or nil on list is treated as a wildcard and matches everything.
func observeOn(on []string, event string) bool {
	if len(on) == 0 {
		return true
	}
	for _, e := range on {
		if e == event {
			return true
		}
	}
	return false
}

// ObserveOn reports whether the entry should fire for the given event type.
// An empty On list means the entry fires for every event.
func (e EventEntry) ObserveOn(event string) bool {
	return observeOn(e.On, event)
}

// ObserveOn reports whether the entry should fire for the given event type.
// An empty On list means the entry fires for every event.
func (w WatchEntry) ObserveOn(event string) bool {
	return observeOn(w.On, event)
}

// ValidObserveEvents returns all known observe event values in declaration order.
func ValidObserveEvents() []string {
	return []string{
		string(ObserveEventCreate),
		string(ObserveEventUpdate),
		string(ObserveEventDelete),
	}
}

// IsValidObserveEvent reports whether s is a known observe event type.
func IsValidObserveEvent(s string) bool {
	switch ObserveEvent(s) {
	case ObserveEventCreate, ObserveEventUpdate, ObserveEventDelete:
		return true
	}
	return false
}

// IsAllValid reports if a slice of observe event strings are valid
func IsAllValid(events []string) (bool, []string) {
	var unknown []string
	for _, event := range events {
		if !IsValidObserveEvent(event) {
			unknown = append(unknown, event)
		}
	}
	if len(unknown) > 0 {
		return false, unknown
	}
	return true, nil
}

// ── WatchEntry ────────────────────────────────────────────────────────────────

// WatchEntry declares a secondary Kubernetes resource Orkestra should watch.
// When the resource changes, Orkestra resolves the relevant primary CR key(s)
// and enqueues them — no Go required.
//
// Key resolution: if the changed object has an ownerReference pointing to a
// primary CR, that CR is enqueued. Otherwise all known CRs of the primary kind
// are enqueued (shared-resource broadcast).
//
// YAML:
//
//	operatorBox:
//	  watch:
//	    - apiVersion: apps/v1
//	      kind: Deployment
//	    - apiVersion: v1
//	      kind: ConfigMap
//	      namespace: my-operator-system
//	      name: shared-config
//	    - apiVersion: v1
//	      kind: Node
//	      on: [update]
type WatchEntry struct {
	// APIVersion is the Kubernetes API version of the resource to watch.
	// e.g. "apps/v1", "v1", "networking.k8s.io/v1"
	APIVersion string `yaml:"apiVersion" json:"apiVersion" validate:"required"`

	// Kind is the Kubernetes Kind of the resource to watch.
	// e.g. "Deployment", "ConfigMap", "Node"
	Kind string `yaml:"kind" json:"kind" validate:"required"`

	// Namespace restricts the watch to a single namespace.
	// When empty the watch is cluster-scoped (all namespaces).
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`

	// Name restricts the watch to a single named resource.
	// When set only events for that specific object trigger the enqueue.
	// Typically used for well-known shared resources (a specific ConfigMap or Secret).
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// On declares which event types trigger the enqueue.
	// Valid values: ObserveEventCreate, ObserveEventUpdate, ObserveEventDelete.
	// When empty all three event types are watched.
	On []string `yaml:"on,omitempty" json:"on,omitempty"`

	// EnqueueGate declares conditions evaluated before enqueueing when the watch
	// fires. Sentinels (e.g. generationChanged) are computed against the watched
	// resource's oldObj / newObj at UpdateFunc time and are valid here.
	// When nil all events that pass the On filter are enqueued.
	EnqueueGate *GateConditions `yaml:"enqueueGate,omitempty" json:"enqueueGate,omitempty"`

	// KeyFrom overrides the default key resolution (ownerReference → broadcast).
	// Declare when the standard mechanisms do not express the mapping you need.
	// When nil the runtime checks ownerReferences first; if none match the primary
	// CRD it broadcasts to all known primary CRs.
	KeyFrom *WatchKeyFrom `yaml:"keyFrom,omitempty" json:"keyFrom,omitempty"`

	// Index declares field-path indexers that Orkestra registers on this watch's
	// informer. Each entry makes client.List(ctx, &list, client.MatchingFields{name: value})
	// serve from the cache instead of making a live API call.
	//
	//  watch:
	//    - apiVersion: v1
	//      kind: ConfigMap
	//      index:
	//        - name: metadata.ownerRef
	//          field: ".metadata.ownerReferences[0].name"
	Index []WatchIndex `yaml:"index,omitempty" json:"index,omitempty"`

	// Include is a path (relative to the katalog file) to a YAML file whose
	// "watch:" list replaces this entry in-place. When set all other fields on
	// this entry are ignored. Cleared after expansion.
	Include string `yaml:"include,omitempty" json:"include,omitempty"`
}

// WatchIndex declares one field-path indexer on a watch: entry informer.
// Name is used as the index key — it must match the key passed to client.MatchingFields.
// Field is a dot-separated JSON path into the watched object (e.g. "spec.owner").
type WatchIndex struct {
	// Name is the index name. Must match the key in client.MatchingFields.
	Name string `yaml:"name" json:"name"`
	// Field is the JSON path to index on (e.g. ".spec.owner", "metadata.labels.app").
	Field string `yaml:"field" json:"field"`
}

// WatchKeyFrom overrides the default ownerReference → broadcast key resolution
// for a watch: entry. Exactly one of Label or Name must be set.
//
//	keyFrom:
//	  label: "app.kubernetes.io/cr-owner"   # label on the watched object carries the key
//
//	keyFrom:
//	  name: "my-singleton-cr"               # always enqueue this named primary CR
//	  namespace: "my-namespace"             # optional; omit for cluster-scoped CRDs
type WatchKeyFrom struct {
	// Label names a label on the watched object whose value is the primary CR key.
	// The value must be a valid Kubernetes key: "namespace/name" or bare "name".
	// Mutually exclusive with Name.
	Label string `yaml:"label,omitempty" json:"label,omitempty"`

	// Name is a fixed primary CR name to enqueue regardless of which watched
	// object changed. Use for singleton operators (one CR per cluster).
	// Mutually exclusive with Label.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Namespace qualifies Name. Omit for cluster-scoped primary CRDs.
	// Ignored when Label is set.
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`
}

// Key returns the enqueue key for the fixed-name variant.
func (kf *WatchKeyFrom) Key() string {
	if kf.Namespace != "" {
		return kf.Namespace + "/" + kf.Name
	}
	return kf.Name
}

// InvalidOnValues returns any On values that are not valid WatchEvent constants.
// Returns nil when all values are valid.
func (w WatchEntry) InvalidOnValues() []string {
	var invalid []string
	for _, e := range w.On {
		if !IsValidObserveEvent(e) {
			invalid = append(invalid, e)
		}
	}
	return invalid
}

// ToManagedResource converts the entry to a ManagedResource suitable for
// ResolveGVR — used for RBAC generation.
func (w WatchEntry) ToManagedResource() domain.ManagedResource {
	return domain.ManagedResource{
		APIVersion: w.APIVersion,
		Kind:       w.Kind,
	}
}

// ToCRDInfo converts a WatchEntry + resolved GVR to a kubeclient.CRDInfo
// for NewDynamicListerWatcher. Namespace is set to the entry's declared namespace;
// Namespaced is true when a namespace is declared (restricts the watch to that
// namespace), false for a cluster-scoped watch (all namespaces).
func (w WatchEntry) ToCRDInfo(gvr schema.GroupVersionResource) kubeclient.CRDInfo {
	return kubeclient.CRDInfo{
		Group:      gvr.Group,
		Version:    gvr.Version,
		Plural:     gvr.Resource,
		Namespace:  w.Namespace,
		Namespaced: w.Namespace != "",
	}
}

// HasWatchSentinels returns true if this watch entry has enqueue gate sentinels
func (w WatchEntry) HasWatchSentinels() bool {
	return w.EnqueueGate != nil && len(w.EnqueueGate.DeclaredSentinels()) > 0
}

// GVKString returns the canonical group/version/kind string for this watch.
func (w *WatchEntry) GVKString() string {
	if w == nil {
		return ""
	}

	gv, err := schema.ParseGroupVersion(w.APIVersion)
	if err != nil {
		return ""
	}

	return schema.GroupVersionKind{
		Group:   gv.Group,
		Version: gv.Version,
		Kind:    w.Kind,
	}.String()
}

// ── EventEntry ────────────────────────────────────────────────────────────────

// EventEntry declares a Kubernetes Event that should wake this operator.
//
// Events are matched by their semantic Event fields. Once an event matches,
// KeyFrom resolves the primary CR key using the same key-selection semantics
// used by WatchEntry. The Event itself remains the observed secondary object.
//
// YAML:
//
//	operatorBox:
//	  events:
//	    - reason: DatabaseReady
//	      regarding:
//	        apiVersion: databases.example.com/v1
//	        kind: Database
//	      keyFrom:
//	        name: my-backup
//
// regarding and related identify objects referenced by the Event. They do not
// become ownerReferences and are not used implicitly for primary-key routing.
type EventEntry struct {
	// Reason matches events.k8s.io/v1 Event.reason.
	Reason string `yaml:"reason,omitempty" json:"reason,omitempty"`

	// Action matches events.k8s.io/v1 Event.action.
	Action string `yaml:"action,omitempty" json:"action,omitempty"`

	// Type matches events.k8s.io/v1 Event.type (for example Normal or Warning).
	Type string `yaml:"type,omitempty" json:"type,omitempty"`

	// ReportingController matches events.k8s.io/v1 Event.reportingController.
	ReportingController string `yaml:"reportingController,omitempty" json:"reportingController,omitempty"`

	// ReportingInstance matches events.k8s.io/v1 Event.reportingInstance.
	ReportingInstance string `yaml:"reportingInstance,omitempty" json:"reportingInstance,omitempty"`

	// Regarding selects the object that the Event is about.
	// It is a matching constraint only; it does not define primary-key routing.
	Regarding *domain.ManagedResource `yaml:"regarding,omitempty" json:"regarding,omitempty"`

	// Related optionally selects the object related to the Event.
	// It is a matching constraint only; it does not define primary-key routing.
	Related *domain.ManagedResource `yaml:"related,omitempty" json:"related,omitempty"`

	// Namespace restricts matching to Events in a single namespace.
	// When empty Events from all namespaces are eligible.
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`

	// Name restricts matching to a single Event by metadata.name.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// On declares which event types trigger the enqueue.
	// Valid values: ObserveEventCreate, ObserveEventUpdate, ObserveEventDelete.
	// When empty all three event types are watched.
	On []string `yaml:"on,omitempty" json:"on,omitempty"`

	// KeyFrom declares how the matching Event is mapped to primary CR key(s).
	// When nil the default WatchEntry routing semantics are used.
	KeyFrom *WatchKeyFrom `yaml:"keyFrom,omitempty" json:"keyFrom,omitempty"`

	// EnqueueGate declares conditions evaluated before enqueueing when the event
	// matches. The Event is the observed object used for admission evaluation.
	EnqueueGate *GateConditions `yaml:"enqueueGate,omitempty" json:"enqueueGate,omitempty"`

	// Include is a path (relative to the katalog file) to a YAML file whose
	// "watch:" list replaces this entry in-place. When set all other fields on
	// this entry are ignored. Cleared after expansion.
	Include string `yaml:"include,omitempty" json:"include,omitempty"`
}

// Matches reports whether an Event matches this declaration.
//
// Empty fields are wildcards.
func (e EventEntry) Matches(obj *unstructured.Unstructured) bool {
	if obj == nil {
		return false
	}

	reason, _, _ := unstructured.NestedString(obj.Object, "reason")
	action, _, _ := unstructured.NestedString(obj.Object, "action")
	eventType, _, _ := unstructured.NestedString(obj.Object, "type")
	reportingController, _, _ := unstructured.NestedString(obj.Object, "reportingController")
	reportingInstance, _, _ := unstructured.NestedString(obj.Object, "reportingInstance")

	if e.Reason != "" && e.Reason != reason {
		return false
	}
	if e.Action != "" && e.Action != action {
		return false
	}
	if e.Type != "" && e.Type != eventType {
		return false
	}
	if e.ReportingController != "" && e.ReportingController != reportingController {
		return false
	}
	if e.ReportingInstance != "" && e.ReportingInstance != reportingInstance {
		return false
	}

	if e.Namespace != "" && e.Namespace != obj.GetNamespace() {
		return false
	}
	if e.Name != "" && e.Name != obj.GetName() {
		return false
	}

	if e.Regarding != nil && !e.Regarding.Matches(obj, "regarding") {
		return false
	}
	if e.Related != nil && !e.Related.Matches(obj, "related") {
		return false
	}

	return true
}

// ToWatchEntry converts an EventEntry into a WatchEntry
func (e EventEntry) ToWatchEntry(crd CRDEntry) WatchEntry {
	return WatchEntry{
		APIVersion:  crd.APIVersion(),
		Kind:        crd.Kind(),
		KeyFrom:     e.KeyFrom,
		EnqueueGate: e.EnqueueGate,
		Name:        e.Name,
		Namespace:   e.Namespace,
	}
}

// HasEventSentinels returns true if this watch entry has enqueue gate sentinels
func (e EventEntry) HasEventSentinels() bool {
	return e.EnqueueGate != nil && len(e.EnqueueGate.DeclaredSentinels()) > 0
}
