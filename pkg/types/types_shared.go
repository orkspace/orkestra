// pkg/types/types_shared.go
package types

// ── Hook template source types — flat format ────────────────────────
//
// All template source types use a single flat field layout.
//
// Instead, Orkestra uses:
//
//	Any string field containing "{{" → treated as a Go text/template expression.
//	                                    Evaluated against the live CR at reconcile time.
//	Any string field without "{{"    → treated as a static value. Used as-is.
//
// This means the same field can hold either a static value or a CR field reference
// without any additional YAML structure:
//
//	image: "nginx:latest"                 static — same for every CR of this type
//	image: "{{ .spec.image }}"            dynamic — resolved from CR spec at reconcile time
//	name:  "{{ .metadata.name }}-app"     dynamic — CR name with a static suffix
//	port:  "8080"                         static integer string
//	port:  "{{ .spec.port }}"             dynamic integer string
//
// Template context is the full CR object as map[string]interface{}:
//
//	.metadata.name        CR name
//	.metadata.namespace   CR namespace
//	.metadata.labels      CR labels map
//	.spec.*               any spec field (dynamic mode only — full spec accessible)
//	.status.*             any status field
//
// All resources created by hook templates receive owner references pointing to the CR.
// This means cascade deletion is automatic — child resources are garbage collected
// when the CR is deleted without requiring explicit onDelete declarations for most cases.

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ── Shared resource value types ───────────────────────────────────────────────

// Labels is a set of Kubernetes labels or annotations. When used in hook template
// declarations, values support Go text/template expressions evaluated against the
// live CR at reconcile time.
// e.g. {app: "{{ .metadata.name }}"}
type Labels map[string]string

// Stringifier
func (l Labels) String() string {
	if len(l) == 0 {
		return ""
	}
	parts := make([]string, 0, len(l))
	for k, v := range l {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func (l Labels) Contains(lbl map[string]string) bool {
	if l.Empty() {
		return false
	}
	for kl, vl := range l {
		for lblKey, lblVaal := range lbl {
			if kl == lblKey && vl == lblVaal {
				return true
			}
		}
	}
	return false
}

func (l Labels) Empty() bool {
	return len(l) == 0
}

// ResourceRequirements mirrors Kubernetes resource requests and limits.
// Values are static Kubernetes quantity strings — template expressions
// are not supported here.
// e.g. requests: {cpu: "100m", memory: "128Mi"}
//
// Profile is mutually exclusive with Requests and Limits. When Profile is set,
// it expands into a complete ResourceRequirements at reconcile time.
type ResourceRequirements struct {
	Profile  string            `yaml:"profile,omitempty" json:"profile,omitempty" validate:"omitempty"`
	Requests map[string]string `yaml:"requests,omitempty" json:"requests,omitempty" validate:"omitempty"`
	Limits   map[string]string `yaml:"limits,omitempty" json:"limits,omitempty" validate:"omitempty"`
}

// EnvVar is a single container environment variable in Kubernetes-native format.
type EnvVar struct {
	Name      string     `yaml:"name" json:"name"`
	Value     string     `yaml:"value,omitempty" json:"value,omitempty"`
	ValueFrom *ValueFrom `yaml:"valueFrom,omitempty" json:"valueFrom,omitempty"`
}

// EnvVarList is a []EnvVar with a custom YAML unmarshaller that detects the
// legacy map format (KEY:\n  value: VAL) and returns a clear migration error.
type EnvVarList []EnvVar

func (l *EnvVarList) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.MappingNode {
		return fmt.Errorf("env must be a list, not a map — each item needs: {name: KEY, value: VAL} or {name: KEY, valueFrom: {...}}")
	}
	type plain []EnvVar
	return value.Decode((*plain)(l))
}

// ValueFrom holds the source of an environment variable value.
// At most one field should be set.
type ValueFrom struct {
	SecretKeyRef    *SecretKeyRef    `yaml:"secretKeyRef,omitempty" json:"secretKeyRef,omitempty"`
	ConfigMapKeyRef *ConfigMapKeyRef `yaml:"configMapKeyRef,omitempty" json:"configMapKeyRef,omitempty"`
}

// EnvFromRef is one secretRef/configMapRef entry under envFrom.
//
// Name, Prefix, and Optional map directly onto Kubernetes' own EnvFromSource /
// SecretEnvSource / ConfigMapEnvSource fields. Keys and Suffix are Orkestra
// extensions with no Kubernetes equivalent — Kubernetes' envFrom is a blanket
// import with no per-key rename mechanism, so a ref that sets Keys is expanded
// into individual env entries at build time instead of a native envFrom source.
// Suffix without Keys is a validation error — see pkg/katalog/validate_envfrom.go.
type EnvFromRef struct {
	Name     string   `yaml:"name" json:"name" validate:"required"`
	Prefix   string   `yaml:"prefix,omitempty" json:"prefix,omitempty"`
	Suffix   string   `yaml:"suffix,omitempty" json:"suffix,omitempty"`
	Optional *bool    `yaml:"optional,omitempty" json:"optional,omitempty"`
	Keys     []string `yaml:"keys,omitempty" json:"keys,omitempty"`
}

// EnvFrom groups environment sources by type.
type EnvFrom struct {
	SecretRef    []EnvFromRef `yaml:"secretRef,omitempty" json:"secretRef,omitempty"`
	ConfigMapRef []EnvFromRef `yaml:"configMapRef,omitempty" json:"configMapRef,omitempty"`
}

// SecretKeyRef selects a key from a Kubernetes Secret.
// Name and Key are required. Optional mirrors corev1.SecretKeySelector.Optional.
type SecretKeyRef struct {
	Name     string `yaml:"name" json:"name"`
	Key      string `yaml:"key" json:"key"`
	Optional *bool  `yaml:"optional,omitempty" json:"optional,omitempty"`
}

// ConfigMapKeyRef selects a key from a Kubernetes ConfigMap.
// Name and Key are required. Optional mirrors corev1.ConfigMapKeySelector.Optional.
type ConfigMapKeyRef struct {
	Name     string `yaml:"name" json:"name"`
	Key      string `yaml:"key" json:"key"`
	Optional *bool  `yaml:"optional,omitempty" json:"optional,omitempty"`
}
