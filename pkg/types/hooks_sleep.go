package types

import (
	"fmt"
	"strings"
)

// SleepEntry describes a single sleep declaration found in a HookTemplates
// resource. It includes the phase (onCreate/onReconcile/onDelete), the
// resource type (for human diagnostics), an optional resource name (if present
// in the template), and the raw duration string.
type SleepEntry struct {
	Phase        string // "onCreate", "onReconcile", "onDelete"
	Resource     string // e.g., "Deployment", "Service", "Job"
	ResourceName string // template name field if available (may be Empty()
	Duration     string // raw duration string as written in the katalog
}

// tiny interfaces used by CollectSleepEntries to avoid a large type switch.
type sleeper interface {
	GetSleep() string
}

type namer interface {
	GetName() string
}

// CollectSleepEntries returns all SleepEntry items declared for this CRD across
// OnCreate, OnReconcile and OnDelete. This is the canonical discovery method
// callers should use for validation, diagnostics, or runtime wiring.
func (c *CRDEntry) CollectSleepEntries() []SleepEntry {
	if !c.HasAnyHookTemplates() {
		return nil
	}

	var out []SleepEntry

	collect := func(phase string, ht *HookTemplates) {
		if ht == nil {
			return
		}
		ht.VisitResources(func(res interface{}) {
			// Prefer the sleeper interface
			si, ok := res.(sleeper)
			if !ok {
				// nothing to do for resources that don't expose GetSleep
				return
			}
			s := si.GetSleep()
			if s == "" {
				return
			}

			// Try to get a friendly resource name via namer
			var rname string
			if n, ok := res.(namer); ok {
				rname = n.GetName()
			}

			// Derive a short resource type from the concrete type
			rtype := fmt.Sprintf("%T", res)
			if i := strings.LastIndex(rtype, "."); i >= 0 {
				rtype = rtype[i+1:]
			}

			out = append(out, SleepEntry{
				Phase:        phase,
				Resource:     rtype,
				ResourceName: rname,
				Duration:     s,
			})
		})
	}

	if c.HasOnCreate() {
		collect("onCreate", c.OperatorBox.OnCreate)
	}
	if c.HasOnReconcile() {
		collect("onReconcile", c.OperatorBox.OnReconcile)
	}
	if c.HasOnDelete() {
		collect("onDelete", c.OperatorBox.OnDelete)
	}

	return out
}

// HasSleep reports whether any sleep declarations exist for this CRD.
// It is a thin convenience wrapper around CollectSleepEntries.
func (c *CRDEntry) HasSleep() bool {
	return len(c.CollectSleepEntries()) > 0
}

// extractSleep attempts to read a Sleep value from any resource template.
// A resource participates if it implements GetSleep(). Returns the raw
// duration string and true when present, otherwise an empty string and false.
func extractSleep(res interface{}) (string, bool) {
	if s, ok := res.(sleeper); ok {
		return s.GetSleep(), true
	}
	return "", false
}

// GetSleep returns the optional artificial delay configured for this resource.
// When non-empty, Orkestra injects the delay at the start of each reconcile
// for latency simulation, autoscale testing, or chaos engineering scenarios.
//
// GetName returns the template's name field when available. These small
// accessors keep CollectSleepEntries concise and avoid a large type switch.
//
// NOTE: value receivers are used so methods are available on both values and pointers.

// Core workload resources
func (t DeploymentTemplateSource) GetSleep() string  { return t.Sleep }
func (t ReplicaSetTemplateSource) GetSleep() string  { return t.Sleep }
func (t StatefulSetTemplateSource) GetSleep() string { return t.Sleep }

// func (t DaemonSetTemplateSource) GetSleep() string      { return t.Sleep }
func (t PodTemplateSource) GetSleep() string { return t.Sleep }

// Services & networking
func (t ServiceTemplateSource) GetSleep() string { return t.Sleep }
func (t IngressTemplateSource) GetSleep() string { return t.Sleep }

func (t NetworkPolicyTemplateSource) GetSleep() string { return t.Sleep }

// Batch
func (t JobTemplateSource) GetSleep() string     { return t.Sleep }
func (t CronJobTemplateSource) GetSleep() string { return t.Sleep }

// Config & identity
func (t SecretTemplateSource) GetSleep() string             { return t.Sleep }
func (t ConfigMapTemplateSource) GetSleep() string          { return t.Sleep }
func (t ServiceAccountTemplateSource) GetSleep() string     { return t.Sleep }
func (t RoleTemplateSource) GetSleep() string               { return t.Sleep }
func (t RoleBindingTemplateSource) GetSleep() string        { return t.Sleep }
func (t ClusterRoleTemplateSource) GetSleep() string        { return t.Sleep }
func (t ClusterRoleBindingTemplateSource) GetSleep() string { return t.Sleep }

// Storage
func (t PVTemplateSource) GetSleep() string  { return t.Sleep }
func (t PVCTemplateSource) GetSleep() string { return t.Sleep }

// Autoscaling, disruption, scheduling
func (t HPATemplateSource) GetSleep() string       { return t.Sleep }
func (t PDBTemplateSource) GetSleep() string       { return t.Sleep }
func (t NamespaceTemplateSource) GetSleep() string { return t.Sleep }

// Custom Resource
func (t CustomResourceTemplateSource) GetSleep() string { return t.Sleep }

// P L A C E H O L D E R S
// func (t PlaceholderSource) GetSleep() string            { return t.Sleep }

// // Storage
// func (t StorageClassTemplateSource) GetSleep() string   { return t.Sleep }
// func (t StorageLocationTemplateSource) GetSleep() string { return t.Sleep }
// func (t StoragePoolTemplateSource) GetSleep() string    { return t.Sleep }
// func (t StorageBackupTemplateSource) GetSleep() string  { return t.Sleep }
// func (t StorageSnapshotTemplateSource) GetSleep() string { return t.Sleep }
// func (t StorageVolumeTemplateSource) GetSleep() string  { return t.Sleep }

// // Scheduling / QoS
// func (t PriorityClassTemplateSource) GetSleep() string  { return t.Sleep }
// func (t RuntimeClassTemplateSource) GetSleep() string   { return t.Sleep }
func (t LimitRangeTemplateSource) GetSleep() string    { return t.Sleep }
func (t ResourceQuotaTemplateSource) GetSleep() string { return t.Sleep }

// func (t PriorityLevelConfigurationTemplateSource) GetSleep() string {
//     return t.Sleep
// }

// // Pod templates and monitors
// func (t PodTemplatePlaceholderSource) GetSleep() string  { return t.Sleep }
// func (t ServiceMonitorTemplateSource) GetSleep() string { return t.Sleep }
// func (t PodSecurityPolicyTemplateSource) GetSleep() string { return t.Sleep }

// Additional common types
func (t PodTemplateSource) GetName() string         { return t.Name }
func (t DeploymentTemplateSource) GetName() string  { return t.Name }
func (t ReplicaSetTemplateSource) GetName() string  { return t.Name }
func (t StatefulSetTemplateSource) GetName() string { return t.Name }

// func (t DaemonSetTemplateSource) GetName() string      { return t.Name }
func (t ServiceTemplateSource) GetName() string { return t.Name }
func (t IngressTemplateSource) GetName() string { return t.Name }

func (t NetworkPolicyTemplateSource) GetName() string      { return t.Name }
func (t JobTemplateSource) GetName() string                { return t.Name }
func (t CronJobTemplateSource) GetName() string            { return t.Name }
func (t SecretTemplateSource) GetName() string             { return t.Name }
func (t ConfigMapTemplateSource) GetName() string          { return t.Name }
func (t ServiceAccountTemplateSource) GetName() string     { return t.Name }
func (t RoleTemplateSource) GetName() string               { return t.Name }
func (t RoleBindingTemplateSource) GetName() string        { return t.Name }
func (t ClusterRoleTemplateSource) GetName() string        { return t.Name }
func (t ClusterRoleBindingTemplateSource) GetName() string { return t.Name }
func (t PVTemplateSource) GetName() string                 { return t.Name }
func (t PVCTemplateSource) GetName() string                { return t.Name }
func (t HPATemplateSource) GetName() string                { return t.Name }
func (t PDBTemplateSource) GetName() string                { return t.Name }
func (t NamespaceTemplateSource) GetName() string          { return t.Name }

// Custom Resource
func (t CustomResourceTemplateSource) GetName() string { return t.Sleep }

// P L A C E H O L D E R S
// func (t PlaceholderSource) GetName() string            { return t.Name }
// func (t StorageClassTemplateSource) GetName() string   { return t.Name }
// func (t StorageLocationTemplateSource) GetName() string { return t.Name }
// func (t StoragePoolTemplateSource) GetName() string    { return t.Name }
// func (t StorageBackupTemplateSource) GetName() string  { return t.Name }
// func (t StorageSnapshotTemplateSource) GetName() string { return t.Name }
// func (t StorageVolumeTemplateSource) GetName() string  { return t.Name }
// func (t PriorityClassTemplateSource) GetName() string  { return t.Name }
// func (t RuntimeClassTemplateSource) GetName() string   { return t.Name }
func (t LimitRangeTemplateSource) GetName() string    { return t.Name }
func (t ResourceQuotaTemplateSource) GetName() string { return t.Name }

// func (t PriorityLevelConfigurationTemplateSource) GetName() string { return t.Name }
// func (t PodTemplatePlaceholderSource) GetName() string  { return t.Name }
// func (t ServiceMonitorTemplateSource) GetName() string { return t.Name }
// func (t PodSecurityPolicyTemplateSource) GetName() string {return t.Name}
