// pkg/template/resolver.go
package template

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/note"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// Resolver evaluates Go text/template expressions against a live CR object.
//
// Any field value containing "{{" is treated as a template expression and
// evaluated against the CR. Any value without "{{" is returned as-is.
//
// Template context is the CR's full object as map[string]interface{}:
//
//	"{{ .metadata.name }}"        → CR name
//	"{{ .metadata.namespace }}"   → CR namespace
//	"{{ .spec.image }}"           → spec.image field value
//	"{{ .spec.replicas }}"        → spec.replicas field value
//	"{{ .metadata.name }}-app"    → CR name with a static suffix
//
// For *unstructured.Unstructured (unstructured mode) the full CR map including
// all spec fields is available.
//
// TYPED:
// For typed objects only metadata fields were accessible — and used Go mode hooks for full spec access.
// Until 'objectToMap' breakthrough
type Resolver struct {
	data           map[string]interface{}
	ownerName      string
	ownerNamespace string
	profiles       orktypes.ProfileRegistry
	// mergedFuncs is set by WithUserNotes and contains orkNotes + user-defined notes.
	// When nil, Resolve() uses the package-level orkNotes FuncMap directly.
	mergedFuncs template.FuncMap
}

// WithSentinels adds the declared sentinel names to the resolver's FuncMap.
// Each sentinel is a no-arg function that returns its computed string value.
//
// Pass nil for values at validate time — sentinels return "" (stub for parse).
// Pass the computed values map at runtime — built by the informer UpdateFunc.
//
// Sentinels layer on top of any existing mergedFuncs (including user notes).
// Templates that reference an undeclared sentinel name fail to parse — this is
// how `ork validate` catches misuse without executing templates.
func (r *Resolver) WithSentinels(declared []string, values map[string]string) *Resolver {
	if len(declared) == 0 {
		return r
	}
	base := orkNotes
	if r.mergedFuncs != nil {
		base = r.mergedFuncs
	}
	merged := make(template.FuncMap, len(base)+len(declared))
	for k, v := range base {
		merged[k] = v
	}
	for _, name := range declared {
		n := name // capture
		merged[n] = func() string { return values[n] }
	}
	r.mergedFuncs = merged
	return r
}

// SentinelFuncMap returns a FuncMap containing stubs for each declared sentinel.
// All stubs return "" — only template parsing is checked, not execution.
// Used by validators that need to parse gate templates without a full Resolver.
func SentinelFuncMap(declared []string) template.FuncMap {
	fm := make(template.FuncMap, len(declared))
	for _, name := range declared {
		n := name
		fm[n] = func() string { return "" }
	}
	return fm
}

// WithProfiles attaches a user-defined profile registry to the resolver.
// Call this after NewResolver when the katalog declares a profiles: block.
func (r *Resolver) WithProfiles(reg orktypes.ProfileRegistry) *Resolver {
	r.profiles = reg
	return r
}

// WithUserNotes registers user-defined notes from the Katalog's NoteRegistry.
// Each note's Expression is compiled as a Go template and registered under its
// Name in the resolver's FuncMap. Calling {{ noteName }} in any template
// evaluates the expression against the current CR's data.
// Built-in notes remain available; user notes may call built-in notes and
// each other inside their expressions.
func (r *Resolver) WithUserNotes(reg orktypes.NoteRegistry) *Resolver {
	if reg.Empty() {
		return r
	}
	// merged holds built-ins + user notes. Closures capture it by reference
	// so all user notes see the complete FuncMap (including each other) at eval time.
	merged := make(template.FuncMap, len(orkNotes)+len(reg.Functions))
	for k, v := range orkNotes {
		merged[k] = v
	}
	for _, n := range reg.Functions {
		expr := n.Expression // capture by value, not loop variable
		merged[n.Name] = func() interface{} {
			tmpl, err := template.New("").Option("missingkey=zero").Funcs(merged).Parse(expr)
			if err != nil {
				return ""
			}
			var buf bytes.Buffer
			_ = tmpl.Execute(&buf, r.data)
			out := strings.TrimSpace(strings.ReplaceAll(buf.String(), "<no value>", ""))
			switch out {
			case "true":
				return true
			case "false":
				return false
			default:
				return out
			}
		}
	}
	r.mergedFuncs = merged
	return r
}

// Profiles returns the user-defined profile registry attached to this resolver.
func (r *Resolver) Profiles() orktypes.ProfileRegistry {
	return r.profiles
}

// NewResolver creates a Resolver from any domain.Object.
func NewResolver(ctx context.Context, obj domain.Object) (*Resolver, error) {
	data, err := objectToMap(obj) // converts any domain.Object to mapstringinterface{} for template execution.
	if err != nil {
		return nil, fmt.Errorf("template.NewResolver: %w", err)
	}

	// Basic defaults -> workaround for now until full implementation
	data["git"] = map[string]interface{}{
		"called":  "false",
		"commit":  "",
		"changed": "false",
		"path":    "",
		"error":   "",
	}

	data["docker"] = map[string]interface{}{
		"called":         "false",
		"image":          "",
		"buildSucceeded": "false",
		"error":          "",
	}

	// Inject empty inputs map so any unexpanded {{ .inputs.* }} motif expressions
	// that survive motif expansion return "" instead of nil-pointer-panicking.
	if _, ok := data["inputs"]; !ok {
		data["inputs"] = map[string]interface{}{}
	}

	return &Resolver{
		data:           data,
		ownerName:      obj.GetName(),
		ownerNamespace: obj.GetNamespace(),
	}, nil
}

// NewResolverFromMap creates a Resolver from a plain map[string]interface{}.
// Used by conversion webhooks where we work with unstructured JSON.
func NewResolverFromMap(data map[string]interface{}) *Resolver {
	return &Resolver{
		data: data,
		// ownerName/ownerNamespace are optional here; only needed for defaults.
	}
}

// ── Performance review from Engr. Kenny ───────────────────────────────────────────────────
//
// note.Map() is called on every Resolve() call that contains "{{".
// Each call allocates a new FuncMap. For high-throughput operators this
// can be optimised by making the FuncMap a package-level variable:
//
//	var orkNotes = note.Map()
//
// And referencing it in Resolve():
//
//	.Funcs(orkNotes)
//
// This is a safe optimisation — note.Map() is a pure function that always
// returns the same map. The template engine does not modify the FuncMap
// after registration.
var orkNotes = note.Map()

// Resolve evaluates a single field value against the CR.
//
// If value contains "{{" it is a template expression — evaluated
// against the full CR map. Otherwise returned as-is (static value, no cost).
// Missing CR fields resolve to "" (missingkey=zero — no error on absent keys).
func (r *Resolver) Resolve(value string) (string, error) {
	out, _, err := r.resolve(value)
	return out, err
}

// Empty reports whether the Resolver has no meaningful settings
func (r *Resolver) Empty() bool {
	return r == nil
}

// ResolveStrict evaluates value like Resolve, but also reports whether any
// referenced field was missing (rendered "<no value>" before stripping).
// Callers that require every referenced field to be present — serve.name,
// serve.namespace — use this to catch composite templates (e.g.
// "{{ .team }}-{{ .environment }}") that would otherwise silently resolve
// to a non-empty but meaningless string like "-" when all fields are absent.
func (r *Resolver) ResolveStrict(value string) (out string, missing bool, err error) {
	return r.resolve(value)
}

func (r *Resolver) resolve(value string) (string, bool, error) {
	if value == "" {
		return "", false, nil
	}

	// Fast path — no template markers, static value
	if !orktypes.IsTemplate(value) {
		return value, false, nil
	}

	funcs := orkNotes
	if r.mergedFuncs != nil {
		funcs = r.mergedFuncs
	}
	tmpl, err := template.New("f").Option("missingkey=zero").
		Funcs(funcs).
		Parse(value)
	if err != nil {
		return "", false, fmt.Errorf("parsing %q: %w", value, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, r.data); err != nil {
		return "", false, fmt.Errorf("executing %q: %w", value, err)
	}

	// missingkey=zero makes missing map keys produce nil (interface{} zero value).
	// Go's text/template renders nil interface{} as "<no value>", not "".
	// Replace all occurrences so callers get "" as documented.
	raw := strings.TrimSpace(buf.String())
	missing := strings.Contains(raw, "<no value>")
	out := strings.ReplaceAll(raw, "<no value>", "")
	return out, missing, nil
}

// resolveResourceRequirements resolves template expressions in resources.profile.
// All other ResourceRequirements fields (Requests, Limits) are static k8s quantity strings — not templates.
func (r *Resolver) resolveResourceRequirements(src *orktypes.ResourceRequirements) (*orktypes.ResourceRequirements, error) {
	if src == nil {
		return nil, nil
	}
	out := *src
	var err error
	if out.Profile, err = r.Resolve(src.Profile); err != nil {
		return nil, fmt.Errorf("resources.profile: %w", err)
	}
	return &out, nil
}

func (r *Resolver) resolveProbeConfig(src *orktypes.ProbeConfig) (*orktypes.ProbeConfig, error) {
	if src == nil {
		return nil, nil
	}
	out := *src
	var err error
	if out.Path, err = r.Resolve(src.Path); err != nil {
		return nil, fmt.Errorf("probe.path: %w", err)
	}
	return &out, nil
}

func (r *Resolver) resolveProbes(src *orktypes.ProbesConfig) (*orktypes.ProbesConfig, error) {
	if src == nil {
		return nil, nil
	}
	out := &orktypes.ProbesConfig{}
	var err error
	if out.Startup, err = r.resolveProbeConfig(src.Startup); err != nil {
		return nil, fmt.Errorf("probes.startup: %w", err)
	}
	if out.Liveness, err = r.resolveProbeConfig(src.Liveness); err != nil {
		return nil, fmt.Errorf("probes.liveness: %w", err)
	}
	if out.Readiness, err = r.resolveProbeConfig(src.Readiness); err != nil {
		return nil, fmt.Errorf("probes.readiness: %w", err)
	}
	return out, nil
}

// ResolvePodTemplate resolves all template expressions in a PodTemplateSource.
// Returns a new PodTemplateSource with all expressions evaluated — safe to pass
// directly to pods.Resolve().
//
// Defaults applied when fields are empty after resolution:
//
//	Name      → ownerName + "-pod"      (applied later in pods.Resolve)
//	Namespace → ownerNamespace          (applied here so downstream has it)
func (r *Resolver) ResolvePodTemplate(src orktypes.PodTemplateSource) (orktypes.PodTemplateSource, error) {
	resolved := orktypes.PodTemplateSource{
		ForceConflict: src.ForceConflict,
	}

	var err error

	if resolved.Probes, err = r.resolveProbes(src.Probes); err != nil {
		return resolved, fmt.Errorf("pod.probes: %w", err)
	}
	if resolved.Resources, err = r.resolveResourceRequirements(src.Resources); err != nil {
		return resolved, fmt.Errorf("pod.resources: %w", err)
	}
	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("pod.name: %w", err)
	}
	if resolved.Image, err = r.Resolve(src.Image); err != nil {
		return resolved, fmt.Errorf("pod.image: %w", err)
	}
	if resolved.ImagePullSecrets, err = r.ResolveStringSlice(src.ImagePullSecrets); err != nil {
		return resolved, fmt.Errorf("pod.imagePullSecrets: %w", err)
	}
	if resolved.Port, err = r.Resolve(src.Port); err != nil {
		return resolved, fmt.Errorf("pod.port: %w", err)
	}

	// Namespace — default to owner namespace when not declared
	ns := src.Namespace
	if ns == "" {
		ns = "{{ .metadata.namespace }}"
	}
	if resolved.Namespace, err = r.Resolve(ns); err != nil {
		return resolved, fmt.Errorf("pod.namespace: %w", err)
	}

	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("pod.labels: %w", err)
	}
	if resolved.Annotations, err = r.ResolveMap(src.Annotations); err != nil {
		return resolved, fmt.Errorf("pod.annotations: %w", err)
	}
	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("pod.sleep: %w", err)
	}

	resolved.SecurityContext = src.SecurityContext
	resolved.PodSecurity = src.PodSecurity

	if resolved.Volumes, err = r.resolveVolumes(src.Volumes); err != nil {
		return resolved, fmt.Errorf("pod.volumes: %w", err)
	}
	if resolved.VolumeMounts, err = r.resolveVolumeMounts(src.VolumeMounts); err != nil {
		return resolved, fmt.Errorf("pod.volumeMounts: %w", err)
	}

	return resolved, nil
}

// ResolveDeploymentTemplate resolves all template expressions in a DeploymentTemplateSource.
// Returns a new DeploymentTemplateSource with all expressions evaluated — safe to pass
// directly to deployments.Resolve().
func (r *Resolver) ResolveDeploymentTemplate(src orktypes.DeploymentTemplateSource) (orktypes.DeploymentTemplateSource, error) {
	resolved := orktypes.DeploymentTemplateSource{
		ForceConflict: src.ForceConflict,
	}

	var err error

	if resolved.Probes, err = r.resolveProbes(src.Probes); err != nil {
		return resolved, fmt.Errorf("deployment.probes: %w", err)
	}
	if resolved.Resources, err = r.resolveResourceRequirements(src.Resources); err != nil {
		return resolved, fmt.Errorf("deployment.resources: %w", err)
	}
	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("deployment.name: %w", err)
	}
	if resolved.Image, err = r.Resolve(src.Image); err != nil {
		return resolved, fmt.Errorf("deployment.image: %w", err)
	}
	if resolved.ImagePullSecrets, err = r.ResolveStringSlice(src.ImagePullSecrets); err != nil {
		return resolved, fmt.Errorf("deployment.imagePullSecrets: %w", err)
	}
	if resolved.Replicas, err = r.Resolve(src.Replicas); err != nil {
		return resolved, fmt.Errorf("deployment.replicas: %w", err)
	}
	if resolved.Port, err = r.Resolve(src.Port); err != nil {
		return resolved, fmt.Errorf("deployment.port: %w", err)
	}

	ns := src.Namespace
	if ns == "" {
		ns = "{{ .metadata.namespace }}"
	}
	if resolved.Namespace, err = r.Resolve(ns); err != nil {
		return resolved, fmt.Errorf("deployment.namespace: %w", err)
	}

	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("deployment.labels: %w", err)
	}
	if resolved.Annotations, err = r.ResolveMap(src.Annotations); err != nil {
		return resolved, fmt.Errorf("deployment.annotations: %w", err)
	}
	// service account name
	if resolved.ServiceAccountName, err = r.Resolve(src.ServiceAccountName); err != nil {
		return resolved, fmt.Errorf("deployment.serviceAccountName: %w", err)
	}
	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("deployment.sleep: %w", err)
	}

	// Env resolution
	if len(src.Env) > 0 {
		resolved.Env = make([]orktypes.EnvVar, 0, len(src.Env))
		for _, v := range src.Env {
			ev := orktypes.EnvVar{Name: v.Name}
			if v.Value != "" {
				if ev.Value, err = r.Resolve(v.Value); err != nil {
					return resolved, fmt.Errorf("env[%s].value: %w", v.Name, err)
				}
			}
			if v.ValueFrom != nil {
				ev.ValueFrom = &orktypes.ValueFrom{}
				if v.ValueFrom.SecretKeyRef != nil {
					name, nerr := r.Resolve(v.ValueFrom.SecretKeyRef.Name)
					if nerr != nil {
						return resolved, fmt.Errorf("env[%s].valueFrom.secretKeyRef.name: %w", v.Name, nerr)
					}
					ev.ValueFrom.SecretKeyRef = &orktypes.SecretKeyRef{Name: name, Key: v.ValueFrom.SecretKeyRef.Key}
				}
				if v.ValueFrom.ConfigMapKeyRef != nil {
					name, nerr := r.Resolve(v.ValueFrom.ConfigMapKeyRef.Name)
					if nerr != nil {
						return resolved, fmt.Errorf("env[%s].valueFrom.configMapKeyRef.name: %w", v.Name, nerr)
					}
					ev.ValueFrom.ConfigMapKeyRef = &orktypes.ConfigMapKeyRef{Name: name, Key: v.ValueFrom.ConfigMapKeyRef.Key}
				}
			}
			resolved.Env = append(resolved.Env, ev)
		}
	}

	// EnvFrom resolution
	if src.EnvFrom != nil {
		resolved.EnvFrom = &orktypes.EnvFrom{}
		var everr error
		if resolved.EnvFrom.SecretRef, everr = r.ResolveEnvFromRefs(src.EnvFrom.SecretRef, "envFrom.secretRef"); everr != nil {
			return resolved, everr
		}
		if resolved.EnvFrom.ConfigMapRef, everr = r.ResolveEnvFromRefs(src.EnvFrom.ConfigMapRef, "envFrom.configMapRef"); everr != nil {
			return resolved, everr
		}
	}

	if src.RollingUpdate != nil {
		ru := *src.RollingUpdate
		resolved.RollingUpdate = &ru
	}

	resolved.SecurityContext = src.SecurityContext
	resolved.PodSecurity = src.PodSecurity

	if resolved.Volumes, err = r.resolveVolumes(src.Volumes); err != nil {
		return resolved, fmt.Errorf("deployment.volumes: %w", err)
	}
	if resolved.VolumeMounts, err = r.resolveVolumeMounts(src.VolumeMounts); err != nil {
		return resolved, fmt.Errorf("deployment.volumeMounts: %w", err)
	}

	resolved.Autoscale = src.Autoscale

	return resolved, nil
}

// ResolveReplicaSetTemplate resolves all template expressions in a ReplicaSetTemplateSource.
// Returns a new ReplicaSetTemplateSource with all expressions evaluated — safe to pass
// directly to replicasets.Resolve().
func (r *Resolver) ResolveReplicaSetTemplate(src orktypes.ReplicaSetTemplateSource) (orktypes.ReplicaSetTemplateSource, error) {
	resolved := orktypes.ReplicaSetTemplateSource{
		ForceConflict: src.ForceConflict,
	}

	var err error

	if resolved.Probes, err = r.resolveProbes(src.Probes); err != nil {
		return resolved, fmt.Errorf("replicaset.probes: %w", err)
	}

	if resolved.Resources, err = r.resolveResourceRequirements(src.Resources); err != nil {
		return resolved, fmt.Errorf("replicaset.resources: %w", err)
	}

	// Name
	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("replicaset.name: %w", err)
	}

	// Image
	if resolved.Image, err = r.Resolve(src.Image); err != nil {
		return resolved, fmt.Errorf("replicaset.image: %w", err)
	}

	// ImagePullSecrets
	if resolved.ImagePullSecrets, err = r.ResolveStringSlice(src.ImagePullSecrets); err != nil {
		return resolved, fmt.Errorf("replicaset.imagePullSecrets: %w", err)
	}

	// Replicas
	if resolved.Replicas, err = r.Resolve(src.Replicas); err != nil {
		return resolved, fmt.Errorf("replicaset.replicas: %w", err)
	}

	// Port
	if resolved.Port, err = r.Resolve(src.Port); err != nil {
		return resolved, fmt.Errorf("replicaset.port: %w", err)
	}

	// Namespace (default to CR namespace)
	ns := src.Namespace
	if ns == "" {
		ns = "{{ .metadata.namespace }}"
	}
	if resolved.Namespace, err = r.Resolve(ns); err != nil {
		return resolved, fmt.Errorf("replicaset.namespace: %w", err)
	}

	// service account name
	if resolved.ServiceAccountName, err = r.Resolve(src.ServiceAccountName); err != nil {
		return resolved, fmt.Errorf("replicaet.serviceAccountName: %w", err)
	}

	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("replicaset.sleep: %w", err)
	}

	// Labels
	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("replicaset.labels: %w", err)
	}

	// Annotations
	if resolved.Annotations, err = r.ResolveMap(src.Annotations); err != nil {
		return resolved, fmt.Errorf("replicaset.annotations: %w", err)
	}

	// Env
	if len(src.Env) > 0 {
		resolved.Env = make([]orktypes.EnvVar, 0, len(src.Env))
		for _, v := range src.Env {
			ev := orktypes.EnvVar{Name: v.Name}
			if v.Value != "" {
				if ev.Value, err = r.Resolve(v.Value); err != nil {
					return resolved, fmt.Errorf("env[%s].value: %w", v.Name, err)
				}
			}
			if v.ValueFrom != nil {
				ev.ValueFrom = &orktypes.ValueFrom{}
				if v.ValueFrom.SecretKeyRef != nil {
					name, nerr := r.Resolve(v.ValueFrom.SecretKeyRef.Name)
					if nerr != nil {
						return resolved, fmt.Errorf("env[%s].valueFrom.secretKeyRef.name: %w", v.Name, nerr)
					}
					ev.ValueFrom.SecretKeyRef = &orktypes.SecretKeyRef{Name: name, Key: v.ValueFrom.SecretKeyRef.Key}
				}
				if v.ValueFrom.ConfigMapKeyRef != nil {
					name, nerr := r.Resolve(v.ValueFrom.ConfigMapKeyRef.Name)
					if nerr != nil {
						return resolved, fmt.Errorf("env[%s].valueFrom.configMapKeyRef.name: %w", v.Name, nerr)
					}
					ev.ValueFrom.ConfigMapKeyRef = &orktypes.ConfigMapKeyRef{Name: name, Key: v.ValueFrom.ConfigMapKeyRef.Key}
				}
			}
			resolved.Env = append(resolved.Env, ev)
		}
	}

	// EnvFrom
	if src.EnvFrom != nil {
		resolved.EnvFrom = &orktypes.EnvFrom{}
		var everr error
		if resolved.EnvFrom.SecretRef, everr = r.ResolveEnvFromRefs(src.EnvFrom.SecretRef, "envFrom.secretRef"); everr != nil {
			return resolved, everr
		}
		if resolved.EnvFrom.ConfigMapRef, everr = r.ResolveEnvFromRefs(src.EnvFrom.ConfigMapRef, "envFrom.configMapRef"); everr != nil {
			return resolved, everr
		}
	}

	if src.RollingUpdate != nil {
		ru := *src.RollingUpdate
		resolved.RollingUpdate = &ru
	}

	resolved.SecurityContext = src.SecurityContext
	resolved.PodSecurity = src.PodSecurity

	if resolved.Volumes, err = r.resolveVolumes(src.Volumes); err != nil {
		return resolved, fmt.Errorf("replicaset.volumes: %w", err)
	}
	if resolved.VolumeMounts, err = r.resolveVolumeMounts(src.VolumeMounts); err != nil {
		return resolved, fmt.Errorf("replicaset.volumeMounts: %w", err)
	}

	resolved.Autoscale = src.Autoscale

	return resolved, nil
}

// ResolveServiceTemplate resolves all template expressions in a ServiceTemplateSource.
func (r *Resolver) ResolveServiceTemplate(src orktypes.ServiceTemplateSource) (orktypes.ServiceTemplateSource, error) {
	resolved := orktypes.ServiceTemplateSource{}

	var err error

	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("service.name: %w", err)
	}
	if resolved.Type, err = r.Resolve(src.Type); err != nil {
		return resolved, fmt.Errorf("service.type: %w", err)
	}
	if resolved.Protocol, err = r.Resolve(src.Protocol); err != nil {
		return resolved, fmt.Errorf("service.protocol: %w", err)
	}
	if resolved.Port, err = r.Resolve(src.Port); err != nil {
		return resolved, fmt.Errorf("service.port: %w", err)
	}
	if resolved.TargetPort, err = r.Resolve(src.TargetPort); err != nil {
		return resolved, fmt.Errorf("service.targetPort: %w", err)
	}
	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("service.sleep: %w", err)
	}

	ns := src.Namespace
	if ns == "" {
		ns = "{{ .metadata.namespace }}"
	}
	if resolved.Namespace, err = r.Resolve(ns); err != nil {
		return resolved, fmt.Errorf("service.namespace: %w", err)
	}

	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("service.labels: %w", err)
	}

	if resolved.Selector, err = r.ResolveMap(src.Selector); err != nil {
		return resolved, fmt.Errorf("service.selector: %w", err)
	}

	return resolved, nil
}

// ResolveNamespaceTemplate resolves all template expressions in a NamespaceTemplateSource.
func (r *Resolver) ResolveNamespaceTemplate(src orktypes.NamespaceTemplateSource) (orktypes.NamespaceTemplateSource, error) {
	resolved := orktypes.NamespaceTemplateSource{}

	var err error

	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("namespace.name: %w", err)
	}

	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("namespace.labels: %w", err)
	}

	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("namespace.sleep: %w", err)
	}

	for i, a := range src.Finalizers {
		rv, e := r.Resolve(a)
		if e != nil {
			return resolved, fmt.Errorf("namespace.finalizers[%d]: %w", i, e)
		}
		resolved.Finalizers = append(resolved.Finalizers, rv)
	}

	return resolved, nil
}

// ResolveJobTemplate resolves all template expressions in a JobTemplateSource.
func (r *Resolver) ResolveJobTemplate(src orktypes.JobTemplateSource) (orktypes.JobTemplateSource, error) {
	resolved := orktypes.JobTemplateSource{
		BackoffLimit: src.BackoffLimit,
	}

	var err error

	if resolved.Resources, err = r.resolveResourceRequirements(src.Resources); err != nil {
		return resolved, fmt.Errorf("job.resources: %w", err)
	}
	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("job.name: %w", err)
	}
	if resolved.Image, err = r.Resolve(src.Image); err != nil {
		return resolved, fmt.Errorf("job.image: %w", err)
	}
	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("job.sleep: %w", err)
	}
	if resolved.ImagePullSecrets, err = r.ResolveStringSlice(src.ImagePullSecrets); err != nil {
		return resolved, fmt.Errorf("job.imagePullSecrets: %w", err)
	}

	ns := src.Namespace
	if ns == "" {
		ns = "{{ .metadata.namespace }}"
	}
	if resolved.Namespace, err = r.Resolve(ns); err != nil {
		return resolved, fmt.Errorf("job.namespace: %w", err)
	}
	// service account name
	if resolved.ServiceAccountName, err = r.Resolve(src.ServiceAccountName); err != nil {
		return resolved, fmt.Errorf("job.serviceAccountName: %w", err)
	}

	// Each command element resolved independently
	for i, c := range src.Command {
		rv, e := r.Resolve(c)
		if e != nil {
			return resolved, fmt.Errorf("job.command[%d]: %w", i, e)
		}
		resolved.Command = append(resolved.Command, rv)
	}
	for i, a := range src.Args {
		rv, e := r.Resolve(a)
		if e != nil {
			return resolved, fmt.Errorf("job.args[%d]: %w", i, e)
		}
		resolved.Args = append(resolved.Args, rv)
	}

	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("job.labels: %w", err)
	}

	resolved.SecurityContext = src.SecurityContext
	resolved.PodSecurity = src.PodSecurity

	if resolved.Volumes, err = r.resolveVolumes(src.Volumes); err != nil {
		return resolved, fmt.Errorf("job.volumes: %w", err)
	}
	if resolved.VolumeMounts, err = r.resolveVolumeMounts(src.VolumeMounts); err != nil {
		return resolved, fmt.Errorf("job.volumeMounts: %w", err)
	}

	return resolved, nil
}

// ResolveSecretTemplate resolves all template expressions in a SecretTemplateSource.
// Returns a new SecretTemplateSource with all expressions evaluated — safe to pass
// directly to secrets.Resolve().
func (r *Resolver) ResolveSecretTemplate(src orktypes.SecretTemplateSource) (orktypes.SecretTemplateSource, error) {
	resolved := orktypes.SecretTemplateSource{}

	var err error

	// Resolve the template expressions
	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("secret.name: %w", err)
	}
	if resolved.FromSecret, err = r.Resolve(src.FromSecret); err != nil {
		return resolved, fmt.Errorf("secret.fromSecret: %w", err)
	}
	if resolved.FromNamespace, err = r.Resolve(src.FromNamespace); err != nil {
		return resolved, fmt.Errorf("secret.fromNamespace: %w", err)
	}
	if resolved.Type, err = r.Resolve(src.Type); err != nil {
		return resolved, fmt.Errorf("secret.type: %w", err)
	}
	if resolved.Namespace, err = r.Resolve(src.Namespace); err != nil {
		return resolved, fmt.Errorf("secret.namespace: %w", err)
	}
	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("secret.sleep: %w", err)
	}
	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("secret.data: %w", err)
	}

	if len(src.Data) > 0 {
		resolved.Data = make(map[string]string, len(src.Data))
		for k, v := range src.Data {
			rv, e := r.Resolve(v)
			if e != nil {
				return resolved, fmt.Errorf("secret.data[%q]: %w", k, e)
			}
			resolved.Data[k] = rv
		}
	}

	// toNamespaces needs special handling.
	// Each element is either:
	//   a) A literal namespace name → resolve as string
	//   b) A template expression that resolves to a string → resolve as string
	//   c) A template expression that resolves to a []interface{} (list field) →
	//      extract each element individually
	//
	// Case (c) is what happens when toNamespaces: ["{{ .spec.targetNamespaces }}"]
	// where .spec.targetNamespaces is a YAML list in the CR.

	for i, v := range src.ToNamespaces {
		if !orktypes.IsTemplate(v) {
			// Static string — no resolution needed
			if v != "" {
				resolved.ToNamespaces = append(resolved.ToNamespaces, v)
			}
			continue
		}

		// Template expression — check if it resolves to a list field
		raw := resolveRawValue(r.data, v)
		switch typed := raw.(type) {
		case []interface{}:
			// List field — extract each string element
			for _, item := range typed {
				if s, ok := item.(string); ok && s != "" {
					resolved.ToNamespaces = append(resolved.ToNamespaces, s)
				}
			}
		case string:
			if typed != "" {
				resolved.ToNamespaces = append(resolved.ToNamespaces, typed)
			}
		default:
			// Fall back to string resolution
			rv, e := r.Resolve(v)
			if e != nil {
				return resolved, fmt.Errorf("secret.toNamespaces[%d]: %w", i, e)
			}
			if rv != "" {
				resolved.ToNamespaces = append(resolved.ToNamespaces, rv)
			}
		}
	}
	return resolved, nil
}

// ResolveConfigMapTemplate resolves all template expressions in a ConfigMapsTemplateSource.
// Returns a new ConfigMapTemplateSource with all expressions evaluated — safe to pass
// directly to configmaps.Resolve().
func (r *Resolver) ResolveConfigMapTemplate(src orktypes.ConfigMapTemplateSource) (orktypes.ConfigMapTemplateSource, error) {
	resolved := orktypes.ConfigMapTemplateSource{}
	var err error

	// Resolve the template expressions
	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("configmap.name: %w", err)
	}
	if resolved.Namespace, err = r.Resolve(src.Namespace); err != nil {
		return resolved, fmt.Errorf("configmap.namespace: %w", err)
	}
	if resolved.FromNamespace, err = r.Resolve(src.FromNamespace); err != nil {
		return resolved, fmt.Errorf("configmap.fromNamespace: %w", err)
	}
	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("configmap.sleep: %w", err)
	}
	if resolved.FromConfigMap, err = r.Resolve(src.FromConfigMap); err != nil {
		return resolved, fmt.Errorf("configmap.fromConfigMap: %w", err)
	}
	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("configmap.labels: %w", err)
	}

	if len(src.Data) > 0 {
		resolved.Data = make(map[string]string, len(src.Data))
		for k, v := range src.Data {
			rv, e := r.Resolve(v)
			if e != nil {
				return resolved, fmt.Errorf("configmap.data[%q]: %w", k, e)
			}
			resolved.Data[k] = rv
		}
	}

	// toNamespaces needs special handling.
	// Each element is either:
	//   a) A literal namespace name → resolve as string
	//   b) A template expression that resolves to a string → resolve as string
	//   c) A template expression that resolves to a []interface{} (list field) →
	//      extract each element individually
	//
	// Case (c) is what happens when toNamespaces: ["{{ .spec.targetNamespaces }}"]
	// where .spec.targetNamespaces is a YAML list in the CR.

	for i, v := range src.ToNamespaces {
		if !orktypes.IsTemplate(v) {
			// Static string — no resolution needed
			if v != "" {
				resolved.ToNamespaces = append(resolved.ToNamespaces, v)
			}
			continue
		}

		// Template expression — check if it resolves to a list field
		raw := resolveRawValue(r.data, v)
		switch typed := raw.(type) {
		case []interface{}:
			// List field — extract each string element
			for _, item := range typed {
				if s, ok := item.(string); ok && s != "" {
					resolved.ToNamespaces = append(resolved.ToNamespaces, s)
				}
			}
		case string:
			if typed != "" {
				resolved.ToNamespaces = append(resolved.ToNamespaces, typed)
			}
		default:
			// Fall back to string resolution
			rv, e := r.Resolve(v)
			if e != nil {
				return resolved, fmt.Errorf("secret.toNamespaces[%d]: %w", i, e)
			}
			if rv != "" {
				resolved.ToNamespaces = append(resolved.ToNamespaces, rv)
			}
		}
	}

	return resolved, nil
}

// ResolveCronJobTemplate resolves all template expressions in a CronJobTemplateSource.
func (r *Resolver) ResolveCronJobTemplate(src orktypes.CronJobTemplateSource) (orktypes.CronJobTemplateSource, error) {
	resolved := orktypes.CronJobTemplateSource{}

	var err error

	if resolved.Resources, err = r.resolveResourceRequirements(src.Resources); err != nil {
		return resolved, fmt.Errorf("cronjob.resources: %w", err)
	}
	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("cronjob.name: %w", err)
	}
	if resolved.Image, err = r.Resolve(src.Image); err != nil {
		return resolved, fmt.Errorf("cronjob.image: %w", err)
	}
	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("cronjob.sleep: %w", err)
	}
	if resolved.ImagePullSecrets, err = r.ResolveStringSlice(src.ImagePullSecrets); err != nil {
		return resolved, fmt.Errorf("cronjob.imagePullSecrets: %w", err)
	}
	if resolved.Schedule, err = r.Resolve(src.Schedule); err != nil {
		return resolved, fmt.Errorf("cronjob.schedule: %w", err)
	}

	ns := src.Namespace
	if ns == "" {
		ns = "{{ .metadata.namespace }}"
	}
	if resolved.Namespace, err = r.Resolve(ns); err != nil {
		return resolved, fmt.Errorf("cronjob.namespace: %w", err)
	}
	// service account name
	if resolved.ServiceAccountName, err = r.Resolve(src.ServiceAccountName); err != nil {
		return resolved, fmt.Errorf("cronjob.serviceAccountName: %w", err)
	}

	for i, c := range src.Command {
		rv, e := r.Resolve(c)
		if e != nil {
			return resolved, fmt.Errorf("cronjob.command[%d]: %w", i, e)
		}
		resolved.Command = append(resolved.Command, rv)
	}
	for i, a := range src.Args {
		rv, e := r.Resolve(a)
		if e != nil {
			return resolved, fmt.Errorf("cronjob.args[%d]: %w", i, e)
		}
		resolved.Args = append(resolved.Args, rv)
	}

	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("cronjob.labels: %w", err)
	}

	resolved.SecurityContext = src.SecurityContext
	resolved.PodSecurity = src.PodSecurity

	return resolved, nil
}

// ResolveServiceAccountTemplate resolves all template expressions in a ServiceAccountTemplateSource.
func (r *Resolver) ResolveServiceAccountTemplate(src orktypes.ServiceAccountTemplateSource) (orktypes.ServiceAccountTemplateSource, error) {
	resolved := orktypes.ServiceAccountTemplateSource{}

	var err error

	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("serviceaccount.name: %w", err)
	}

	ns := src.Namespace
	if ns == "" {
		ns = "{{ .metadata.namespace }}"
	}
	if resolved.Namespace, err = r.Resolve(ns); err != nil {
		return resolved, fmt.Errorf("serviceaccount.namespace: %w", err)
	}
	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("serviceAccount.sleep: %w", err)
	}

	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("serviceaccount.labels: %w", err)
	}

	return resolved, nil
}

// ResolveRoleTemplate resolves all template expressions in a RoleTemplateSource.
func (r *Resolver) ResolveRoleTemplate(src orktypes.RoleTemplateSource) (orktypes.RoleTemplateSource, error) {
	resolved := orktypes.RoleTemplateSource{
		Reconcile: src.Reconcile,
	}

	var err error

	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("role.name: %w", err)
	}

	ns := src.Namespace
	if ns == "" {
		ns = "{{ .metadata.namespace }}"
	}
	if resolved.Namespace, err = r.Resolve(ns); err != nil {
		return resolved, fmt.Errorf("role.namespace: %w", err)
	}
	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("role.sleep: %w", err)
	}

	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("role.labels: %w", err)
	}

	// Resolve resourceNames in each rule (apiGroups/resources/verbs are typically static)
	for _, rule := range src.Rules {
		resolvedRule := orktypes.PolicyRuleSpec{
			APIGroups: rule.APIGroups,
			Resources: rule.Resources,
			Verbs:     rule.Verbs,
		}
		for _, rn := range rule.ResourceNames {
			rv, resolveErr := r.Resolve(rn)
			if resolveErr != nil {
				return resolved, fmt.Errorf("role.rules.resourceNames: %w", resolveErr)
			}
			resolvedRule.ResourceNames = append(resolvedRule.ResourceNames, rv)
		}
		resolved.Rules = append(resolved.Rules, resolvedRule)
	}

	return resolved, nil
}

// ResolveRoleBindingTemplate resolves all template expressions in a RoleBindingTemplateSource.
func (r *Resolver) ResolveRoleBindingTemplate(src orktypes.RoleBindingTemplateSource) (orktypes.RoleBindingTemplateSource, error) {
	resolved := orktypes.RoleBindingTemplateSource{
		Reconcile: src.Reconcile,
	}

	var err error

	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("rolebinding.name: %w", err)
	}

	ns := src.Namespace
	if ns == "" {
		ns = "{{ .metadata.namespace }}"
	}
	if resolved.Namespace, err = r.Resolve(ns); err != nil {
		return resolved, fmt.Errorf("rolebinding.namespace: %w", err)
	}
	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("rolebinding.sleep: %w", err)
	}

	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("rolebinding.labels: %w", err)
	}

	resolved.RoleRef.Kind = src.RoleRef.Kind
	if resolved.RoleRef.Name, err = r.Resolve(src.RoleRef.Name); err != nil {
		return resolved, fmt.Errorf("rolebinding.roleRef.name: %w", err)
	}

	for i, s := range src.Subjects {
		rs := orktypes.SubjectSpec{Kind: s.Kind}
		if rs.Name, err = r.Resolve(s.Name); err != nil {
			return resolved, fmt.Errorf("rolebinding.subjects[%d].name: %w", i, err)
		}
		if rs.Namespace, err = r.Resolve(s.Namespace); err != nil {
			return resolved, fmt.Errorf("rolebinding.subjects[%d].namespace: %w", i, err)
		}
		resolved.Subjects = append(resolved.Subjects, rs)
	}

	return resolved, nil
}

// ResolveIngressTemplate resolves all template expressions in an IngressTemplateSource.
func (r *Resolver) ResolveIngressTemplate(src orktypes.IngressTemplateSource) (orktypes.IngressTemplateSource, error) {
	resolved := orktypes.IngressTemplateSource{}

	var err error

	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("ingress.name: %w", err)
	}
	if resolved.Host, err = r.Resolve(src.Host); err != nil {
		return resolved, fmt.Errorf("ingress.host: %w", err)
	}
	if resolved.ServiceName, err = r.Resolve(src.ServiceName); err != nil {
		return resolved, fmt.Errorf("ingress.serviceName: %w", err)
	}
	if resolved.ServicePort, err = r.Resolve(src.ServicePort); err != nil {
		return resolved, fmt.Errorf("ingress.servicePort: %w", err)
	}
	if resolved.Path, err = r.Resolve(src.Path); err != nil {
		return resolved, fmt.Errorf("ingress.path: %w", err)
	}
	if resolved.PathType, err = r.Resolve(src.PathType); err != nil {
		return resolved, fmt.Errorf("ingress.pathType: %w", err)
	}
	if resolved.IngressClass, err = r.Resolve(src.IngressClass); err != nil {
		return resolved, fmt.Errorf("ingress.ingressClass: %w", err)
	}
	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("ingress.sleep: %w", err)
	}

	ns := src.Namespace
	if ns == "" {
		ns = "{{ .metadata.namespace }}"
	}
	if resolved.Namespace, err = r.Resolve(ns); err != nil {
		return resolved, fmt.Errorf("ingress.namespace: %w", err)
	}

	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("ingress.labels: %w", err)
	}
	if resolved.Annotations, err = r.ResolveMap(src.Annotations); err != nil {
		return resolved, fmt.Errorf("ingress.annotations: %w", err)
	}

	if src.TLS != nil {
		resolvedTLS := &orktypes.IngressTLSSpec{
			Create:   src.TLS.Create,
			ValidFor: src.TLS.ValidFor,
		}
		if resolvedTLS.SecretName, err = r.Resolve(src.TLS.SecretName); err != nil {
			return resolved, fmt.Errorf("ingress.tls.secretName: %w", err)
		}
		for i, h := range src.TLS.Hosts {
			rv, e := r.Resolve(h)
			if e != nil {
				return resolved, fmt.Errorf("ingress.tls.hosts[%d]: %w", i, e)
			}
			resolvedTLS.Hosts = append(resolvedTLS.Hosts, rv)
		}
		resolved.TLS = resolvedTLS
	}

	return resolved, nil
}

// ResolveHPATemplate resolves all template expressions in an HPATemplateSource.
func (r *Resolver) ResolveHPATemplate(src orktypes.HPATemplateSource) (orktypes.HPATemplateSource, error) {
	resolved := orktypes.HPATemplateSource{}

	var err error

	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("hpa.name: %w", err)
	}
	if resolved.ScaleTargetRef.APIVersion, err = r.Resolve(src.ScaleTargetRef.APIVersion); err != nil {
		return resolved, fmt.Errorf("hpa.scaledRef.apiVersion: %w", err)
	}
	if resolved.ScaleTargetRef.Kind, err = r.Resolve(src.ScaleTargetRef.Kind); err != nil {
		return resolved, fmt.Errorf("hpa.scaledRef.kind: %w", err)
	}
	if resolved.ScaleTargetRef.Name, err = r.Resolve(src.ScaleTargetRef.Name); err != nil {
		return resolved, fmt.Errorf("hpa.scaledRef.name: %w", err)
	}
	if resolved.MinReplicas, err = r.Resolve(src.MinReplicas); err != nil {
		return resolved, fmt.Errorf("hpa.minReplicas: %w", err)
	}
	if resolved.MaxReplicas, err = r.Resolve(src.MaxReplicas); err != nil {
		return resolved, fmt.Errorf("hpa.maxReplicas: %w", err)
	}
	if resolved.TargetCPUUtilizationPercentage, err = r.Resolve(src.TargetCPUUtilizationPercentage); err != nil {
		return resolved, fmt.Errorf("hpa.targetCPUUtilizationPercentage: %w", err)
	}
	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("hpa.sleep: %w", err)
	}

	ns := src.Namespace
	if ns == "" {
		ns = "{{ .metadata.namespace }}"
	}
	if resolved.Namespace, err = r.Resolve(ns); err != nil {
		return resolved, fmt.Errorf("hpa.namespace: %w", err)
	}

	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("hpa.labels: %w", err)
	}

	if src.Behavior != nil {
		b := *src.Behavior
		resolved.Behavior = &b
	}

	return resolved, nil
}

// ResolvePDBTemplate resolves all template expressions in a PDBTemplateSource.
func (r *Resolver) ResolvePDBTemplate(src orktypes.PDBTemplateSource) (orktypes.PDBTemplateSource, error) {
	resolved := orktypes.PDBTemplateSource{}

	var err error

	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("pdb.name: %w", err)
	}
	if resolved.MinAvailable, err = r.Resolve(src.MinAvailable); err != nil {
		return resolved, fmt.Errorf("pdb.minAvailable: %w", err)
	}
	if resolved.MaxUnavailable, err = r.Resolve(src.MaxUnavailable); err != nil {
		return resolved, fmt.Errorf("pdb.maxUnavailable: %w", err)
	}

	ns := src.Namespace
	if ns == "" {
		ns = "{{ .metadata.namespace }}"
	}
	if resolved.Namespace, err = r.Resolve(ns); err != nil {
		return resolved, fmt.Errorf("pdb.namespace: %w", err)
	}

	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("pdb.labels: %w", err)
	}

	if resolved.Selector, err = r.ResolveMap(src.Selector); err != nil {
		return resolved, fmt.Errorf("pdb.selector: %w", err)
	}

	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("pdb.sleep: %w", err)
	}

	if src.Behavior != nil {
		b := *src.Behavior
		resolved.Behavior = &b
	}

	return resolved, nil
}

// ResolveStatefulSetTemplate resolves all template expressions in a StatefulSetTemplateSource.
func (r *Resolver) ResolveStatefulSetTemplate(src orktypes.StatefulSetTemplateSource) (orktypes.StatefulSetTemplateSource, error) {
	resolved := orktypes.StatefulSetTemplateSource{}

	var err error

	if resolved.Probes, err = r.resolveProbes(src.Probes); err != nil {
		return resolved, fmt.Errorf("statefulset.probes: %w", err)
	}
	if resolved.Resources, err = r.resolveResourceRequirements(src.Resources); err != nil {
		return resolved, fmt.Errorf("statefulset.resources: %w", err)
	}
	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("statefulset.name: %w", err)
	}
	if resolved.Image, err = r.Resolve(src.Image); err != nil {
		return resolved, fmt.Errorf("statefulset.image: %w", err)
	}
	if resolved.ImagePullSecrets, err = r.ResolveStringSlice(src.ImagePullSecrets); err != nil {
		return resolved, fmt.Errorf("statefulset.imagePullSecrets: %w", err)
	}
	if resolved.Tag, err = r.Resolve(src.Tag); err != nil {
		return resolved, fmt.Errorf("statefulset.tag: %w", err)
	}
	if resolved.Replicas, err = r.Resolve(src.Replicas); err != nil {
		return resolved, fmt.Errorf("statefulset.replicas: %w", err)
	}
	if resolved.Port, err = r.Resolve(src.Port); err != nil {
		return resolved, fmt.Errorf("statefulset.port: %w", err)
	}
	if resolved.ServiceName, err = r.Resolve(src.ServiceName); err != nil {
		return resolved, fmt.Errorf("statefulset.serviceName: %w", err)
	}
	for i, vct := range src.VolumeClaimTemplates {
		rv := orktypes.VolumeClaimTemplateSource{AccessModes: vct.AccessModes}
		if rv.Name, err = r.Resolve(vct.Name); err != nil {
			return resolved, fmt.Errorf("statefulset.volumeClaimTemplates[%d].name: %w", i, err)
		}
		if rv.StorageClass, err = r.Resolve(vct.StorageClass); err != nil {
			return resolved, fmt.Errorf("statefulset.volumeClaimTemplates[%d].storageClass: %w", i, err)
		}
		if rv.StorageSize, err = r.Resolve(vct.StorageSize); err != nil {
			return resolved, fmt.Errorf("statefulset.volumeClaimTemplates[%d].storageSize: %w", i, err)
		}
		if rv.MountPath, err = r.Resolve(vct.MountPath); err != nil {
			return resolved, fmt.Errorf("statefulset.volumeClaimTemplates[%d].mountPath: %w", i, err)
		}
		resolved.VolumeClaimTemplates = append(resolved.VolumeClaimTemplates, rv)
	}
	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("statefulset.sleep: %w", err)
	}

	ns := src.Namespace
	if ns == "" {
		ns = "{{ .metadata.namespace }}"
	}
	if resolved.Namespace, err = r.Resolve(ns); err != nil {
		return resolved, fmt.Errorf("statefulset.namespace: %w", err)
	}
	// service account name
	if resolved.ServiceAccountName, err = r.Resolve(src.ServiceAccountName); err != nil {
		return resolved, fmt.Errorf("statefulset.serviceAccountName: %w", err)
	}

	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("statefulset.labels: %w", err)
	}
	if resolved.Annotations, err = r.ResolveMap(src.Annotations); err != nil {
		return resolved, fmt.Errorf("statefulset.annotations: %w", err)
	}

	// Env resolution
	if len(src.Env) > 0 {
		resolved.Env = make([]orktypes.EnvVar, 0, len(src.Env))
		for _, v := range src.Env {
			ev := orktypes.EnvVar{Name: v.Name}
			if v.Value != "" {
				if ev.Value, err = r.Resolve(v.Value); err != nil {
					return resolved, fmt.Errorf("env[%s].value: %w", v.Name, err)
				}
			}
			if v.ValueFrom != nil {
				ev.ValueFrom = &orktypes.ValueFrom{}
				if v.ValueFrom.SecretKeyRef != nil {
					name, nerr := r.Resolve(v.ValueFrom.SecretKeyRef.Name)
					if nerr != nil {
						return resolved, fmt.Errorf("env[%s].valueFrom.secretKeyRef.name: %w", v.Name, nerr)
					}
					ev.ValueFrom.SecretKeyRef = &orktypes.SecretKeyRef{Name: name, Key: v.ValueFrom.SecretKeyRef.Key}
				}
				if v.ValueFrom.ConfigMapKeyRef != nil {
					name, nerr := r.Resolve(v.ValueFrom.ConfigMapKeyRef.Name)
					if nerr != nil {
						return resolved, fmt.Errorf("env[%s].valueFrom.configMapKeyRef.name: %w", v.Name, nerr)
					}
					ev.ValueFrom.ConfigMapKeyRef = &orktypes.ConfigMapKeyRef{Name: name, Key: v.ValueFrom.ConfigMapKeyRef.Key}
				}
			}
			resolved.Env = append(resolved.Env, ev)
		}
	}

	// EnvFrom resolution
	if src.EnvFrom != nil {
		resolved.EnvFrom = &orktypes.EnvFrom{}
		var everr error
		if resolved.EnvFrom.SecretRef, everr = r.ResolveEnvFromRefs(src.EnvFrom.SecretRef, "envFrom.secretRef"); everr != nil {
			return resolved, everr
		}
		if resolved.EnvFrom.ConfigMapRef, everr = r.ResolveEnvFromRefs(src.EnvFrom.ConfigMapRef, "envFrom.configMapRef"); everr != nil {
			return resolved, everr
		}
	}

	if src.RollingUpdate != nil {
		ru := *src.RollingUpdate
		resolved.RollingUpdate = &ru
	}

	resolved.SecurityContext = src.SecurityContext
	resolved.PodSecurity = src.PodSecurity

	if resolved.Volumes, err = r.resolveVolumes(src.Volumes); err != nil {
		return resolved, fmt.Errorf("statefulset.volumes: %w", err)
	}
	if resolved.VolumeMounts, err = r.resolveVolumeMounts(src.VolumeMounts); err != nil {
		return resolved, fmt.Errorf("statefulset.volumeMounts: %w", err)
	}

	resolved.Autoscale = src.Autoscale

	return resolved, nil
}

// ResolvePVCTemplate resolves all template expressions in a PVCTemplateSource.
func (r *Resolver) ResolvePVCTemplate(src orktypes.PVCTemplateSource) (orktypes.PVCTemplateSource, error) {
	resolved := orktypes.PVCTemplateSource{
		AccessModes: src.AccessModes,
		VolumeMode:  src.VolumeMode,
	}

	var err error

	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("pvc.name: %w", err)
	}
	if resolved.StorageClassName, err = r.Resolve(src.StorageClassName); err != nil {
		return resolved, fmt.Errorf("pvc.storageClassName: %w", err)
	}
	if resolved.Storage, err = r.Resolve(src.Storage); err != nil {
		return resolved, fmt.Errorf("pvc.storage: %w", err)
	}
	if resolved.VolumeName, err = r.Resolve(src.VolumeName); err != nil {
		return resolved, fmt.Errorf("pvc.volumeName: %w", err)
	}
	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("pvc.sleep: %w", err)
	}

	ns := src.Namespace
	if ns == "" {
		ns = "{{ .metadata.namespace }}"
	}
	if resolved.Namespace, err = r.Resolve(ns); err != nil {
		return resolved, fmt.Errorf("pvc.namespace: %w", err)
	}

	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("pvc.labels: %w", err)
	}

	return resolved, nil
}

// ResolvePVTemplate resolves all template expressions in a PVTemplateSource.
func (r *Resolver) ResolvePVTemplate(src orktypes.PVTemplateSource) (orktypes.PVTemplateSource, error) {
	resolved := orktypes.PVTemplateSource{
		AccessModes: src.AccessModes,
	}

	var err error

	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("pv.name: %w", err)
	}
	if resolved.StorageClassName, err = r.Resolve(src.StorageClassName); err != nil {
		return resolved, fmt.Errorf("pv.storageClassName: %w", err)
	}
	if resolved.Capacity, err = r.Resolve(src.Capacity); err != nil {
		return resolved, fmt.Errorf("pv.capacity: %w", err)
	}
	if resolved.ReclaimPolicy, err = r.Resolve(src.ReclaimPolicy); err != nil {
		return resolved, fmt.Errorf("pv.reclaimPolicy: %w", err)
	}
	if resolved.HostPath, err = r.Resolve(src.HostPath); err != nil {
		return resolved, fmt.Errorf("pv.hostPath: %w", err)
	}
	if resolved.CSIDriver, err = r.Resolve(src.CSIDriver); err != nil {
		return resolved, fmt.Errorf("pv.csiDriver: %w", err)
	}
	if resolved.CSIVolumeHandle, err = r.Resolve(src.CSIVolumeHandle); err != nil {
		return resolved, fmt.Errorf("pv.csiVolumeHandle: %w", err)
	}
	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("pv.sleep: %w", err)
	}

	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("pv.labels: %w", err)
	}

	return resolved, nil
}

// ── Volume / VolumeMount resolution helpers ───────────────────────────────────

// resolveVolumes resolves template expressions in volume name fields.
// ConfigMap name, Secret name, and PVC claimName all support template expressions.
func (r *Resolver) resolveVolumes(src []orktypes.VolumeSource) ([]orktypes.VolumeSource, error) {
	if len(src) == 0 {
		return nil, nil
	}
	result := make([]orktypes.VolumeSource, 0, len(src))
	for i, v := range src {
		rv := v
		var err error
		if rv.Name, err = r.Resolve(v.Name); err != nil {
			return nil, fmt.Errorf("volume[%d].name: %w", i, err)
		}
		if v.ConfigMap != nil {
			cm := *v.ConfigMap
			if cm.Name, err = r.Resolve(v.ConfigMap.Name); err != nil {
				return nil, fmt.Errorf("volume[%d].configMap.name: %w", i, err)
			}
			rv.ConfigMap = &cm
		}
		if v.Secret != nil {
			s := *v.Secret
			if s.Name, err = r.Resolve(v.Secret.Name); err != nil {
				return nil, fmt.Errorf("volume[%d].secret.name: %w", i, err)
			}
			rv.Secret = &s
		}
		if v.PersistentVolumeClaim != nil {
			pvc := *v.PersistentVolumeClaim
			if pvc.ClaimName, err = r.Resolve(v.PersistentVolumeClaim.ClaimName); err != nil {
				return nil, fmt.Errorf("volume[%d].persistentVolumeClaim.claimName: %w", i, err)
			}
			rv.PersistentVolumeClaim = &pvc
		}
		result = append(result, rv)
	}
	return result, nil
}

// resolveVolumeMounts resolves template expressions in mount name, mountPath, and subPath.
func (r *Resolver) resolveVolumeMounts(src []orktypes.VolumeMount) ([]orktypes.VolumeMount, error) {
	if len(src) == 0 {
		return nil, nil
	}
	result := make([]orktypes.VolumeMount, 0, len(src))
	for i, m := range src {
		rm := m
		var err error
		if rm.Name, err = r.Resolve(m.Name); err != nil {
			return nil, fmt.Errorf("volumeMount[%d].name: %w", i, err)
		}
		if rm.MountPath, err = r.Resolve(m.MountPath); err != nil {
			return nil, fmt.Errorf("volumeMount[%d].mountPath: %w", i, err)
		}
		if rm.SubPath, err = r.Resolve(m.SubPath); err != nil {
			return nil, fmt.Errorf("volumeMount[%d].subPath: %w", i, err)
		}
		result = append(result, rm)
	}
	return result, nil
}

// ResolveNetworkPolicyTemplate resolves all template expressions in a NetworkPolicyTemplateSource.
func (r *Resolver) ResolveNetworkPolicyTemplate(src orktypes.NetworkPolicyTemplateSource) (orktypes.NetworkPolicyTemplateSource, error) {
	resolved := orktypes.NetworkPolicyTemplateSource{
		PolicyTypes: src.PolicyTypes,
		Ingress:     src.Ingress,
		Egress:      src.Egress,
		PodSelector: src.PodSelector,
	}
	var err error

	if resolved.Profile, err = r.Resolve(src.Profile); err != nil {
		return resolved, fmt.Errorf("networkpolicy.profile: %w", err)
	}
	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("networkpolicy.name: %w", err)
	}
	if resolved.Namespace, err = r.Resolve(src.Namespace); err != nil {
		return resolved, fmt.Errorf("networkpolicy.namespace: %w", err)
	}
	if resolved.FromNetworkPolicy, err = r.Resolve(src.FromNetworkPolicy); err != nil {
		return resolved, fmt.Errorf("networkpolicy.fromNetworkPolicy: %w", err)
	}
	if resolved.FromNamespace, err = r.Resolve(src.FromNamespace); err != nil {
		return resolved, fmt.Errorf("networkpolicy.fromNamespace: %w", err)
	}
	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("networkpolicy.sleep: %w", err)
	}
	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("networkpolicy.labels: %w", err)
	}

	for i, v := range src.ToNamespaces {
		if !orktypes.IsTemplate(v) {
			if v != "" {
				resolved.ToNamespaces = append(resolved.ToNamespaces, v)
			}
			continue
		}
		raw := resolveRawValue(r.data, v)
		switch typed := raw.(type) {
		case []interface{}:
			for _, item := range typed {
				if s, ok := item.(string); ok && s != "" {
					resolved.ToNamespaces = append(resolved.ToNamespaces, s)
				}
			}
		case string:
			if typed != "" {
				resolved.ToNamespaces = append(resolved.ToNamespaces, typed)
			}
		default:
			rv, e := r.Resolve(v)
			if e != nil {
				return resolved, fmt.Errorf("networkpolicy.toNamespaces[%d]: %w", i, e)
			}
			if rv != "" {
				resolved.ToNamespaces = append(resolved.ToNamespaces, rv)
			}
		}
	}

	return resolved, nil
}

// ResolveResourceQuotaTemplate resolves all template expressions in a ResourceQuotaTemplateSource.
func (r *Resolver) ResolveResourceQuotaTemplate(src orktypes.ResourceQuotaTemplateSource) (orktypes.ResourceQuotaTemplateSource, error) {
	resolved := orktypes.ResourceQuotaTemplateSource{}
	var err error

	if resolved.Profile, err = r.Resolve(src.Profile); err != nil {
		return resolved, fmt.Errorf("resourcequota.profile: %w", err)
	}
	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("resourcequota.name: %w", err)
	}
	if resolved.Namespace, err = r.Resolve(src.Namespace); err != nil {
		return resolved, fmt.Errorf("resourcequota.namespace: %w", err)
	}
	if resolved.FromResourceQuota, err = r.Resolve(src.FromResourceQuota); err != nil {
		return resolved, fmt.Errorf("resourcequota.fromResourceQuota: %w", err)
	}
	if resolved.FromNamespace, err = r.Resolve(src.FromNamespace); err != nil {
		return resolved, fmt.Errorf("resourcequota.fromNamespace: %w", err)
	}
	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("resourcequota.sleep: %w", err)
	}
	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("resourcequota.labels: %w", err)
	}

	if len(src.Hard) > 0 {
		resolved.Hard = make(map[string]string, len(src.Hard))
		for k, v := range src.Hard {
			rv, e := r.Resolve(v)
			if e != nil {
				return resolved, fmt.Errorf("resourcequota.hard[%q]: %w", k, e)
			}
			resolved.Hard[k] = rv
		}
	}

	for i, v := range src.ToNamespaces {
		if !orktypes.IsTemplate(v) {
			if v != "" {
				resolved.ToNamespaces = append(resolved.ToNamespaces, v)
			}
			continue
		}
		raw := resolveRawValue(r.data, v)
		switch typed := raw.(type) {
		case []interface{}:
			for _, item := range typed {
				if s, ok := item.(string); ok && s != "" {
					resolved.ToNamespaces = append(resolved.ToNamespaces, s)
				}
			}
		case string:
			if typed != "" {
				resolved.ToNamespaces = append(resolved.ToNamespaces, typed)
			}
		default:
			rv, e := r.Resolve(v)
			if e != nil {
				return resolved, fmt.Errorf("resourcequota.toNamespaces[%d]: %w", i, e)
			}
			if rv != "" {
				resolved.ToNamespaces = append(resolved.ToNamespaces, rv)
			}
		}
	}

	return resolved, nil
}

// ResolveLimitRangeTemplate resolves all template expressions in a LimitRangeTemplateSource.
func (r *Resolver) ResolveLimitRangeTemplate(src orktypes.LimitRangeTemplateSource) (orktypes.LimitRangeTemplateSource, error) {
	resolved := orktypes.LimitRangeTemplateSource{}
	var err error

	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("limitrange.name: %w", err)
	}
	if resolved.Namespace, err = r.Resolve(src.Namespace); err != nil {
		return resolved, fmt.Errorf("limitrange.namespace: %w", err)
	}
	if resolved.FromLimitRange, err = r.Resolve(src.FromLimitRange); err != nil {
		return resolved, fmt.Errorf("limitrange.fromLimitRange: %w", err)
	}
	if resolved.FromNamespace, err = r.Resolve(src.FromNamespace); err != nil {
		return resolved, fmt.Errorf("limitrange.fromNamespace: %w", err)
	}
	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("limitrange.sleep: %w", err)
	}
	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("limitrange.labels: %w", err)
	}

	for i, item := range src.Limits {
		ri := item
		ri.Max, err = r.resolveStringMap(item.Max)
		if err != nil {
			return resolved, fmt.Errorf("limitrange.limits[%d].max: %w", i, err)
		}
		ri.Min, err = r.resolveStringMap(item.Min)
		if err != nil {
			return resolved, fmt.Errorf("limitrange.limits[%d].min: %w", i, err)
		}
		ri.Default, err = r.resolveStringMap(item.Default)
		if err != nil {
			return resolved, fmt.Errorf("limitrange.limits[%d].default: %w", i, err)
		}
		ri.DefaultRequest, err = r.resolveStringMap(item.DefaultRequest)
		if err != nil {
			return resolved, fmt.Errorf("limitrange.limits[%d].defaultRequest: %w", i, err)
		}
		ri.MaxLimitRequestRatio, err = r.resolveStringMap(item.MaxLimitRequestRatio)
		if err != nil {
			return resolved, fmt.Errorf("limitrange.limits[%d].maxLimitRequestRatio: %w", i, err)
		}
		resolved.Limits = append(resolved.Limits, ri)
	}

	for i, v := range src.ToNamespaces {
		if !orktypes.IsTemplate(v) {
			if v != "" {
				resolved.ToNamespaces = append(resolved.ToNamespaces, v)
			}
			continue
		}
		raw := resolveRawValue(r.data, v)
		switch typed := raw.(type) {
		case []interface{}:
			for _, item := range typed {
				if s, ok := item.(string); ok && s != "" {
					resolved.ToNamespaces = append(resolved.ToNamespaces, s)
				}
			}
		case string:
			if typed != "" {
				resolved.ToNamespaces = append(resolved.ToNamespaces, typed)
			}
		default:
			rv, e := r.Resolve(v)
			if e != nil {
				return resolved, fmt.Errorf("limitrange.toNamespaces[%d]: %w", i, e)
			}
			if rv != "" {
				resolved.ToNamespaces = append(resolved.ToNamespaces, rv)
			}
		}
	}

	return resolved, nil
}

// resolveStringMap resolves template expressions in all values of a map[string]string.
func (r *Resolver) resolveStringMap(m map[string]string) (map[string]string, error) {
	if len(m) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		rv, err := r.Resolve(v)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", k, err)
		}
		out[k] = rv
	}
	return out, nil
}

// ResolveClusterRoleTemplate resolves all template expressions in a ClusterRoleTemplateSource.
func (r *Resolver) ResolveClusterRoleTemplate(src orktypes.ClusterRoleTemplateSource) (orktypes.ClusterRoleTemplateSource, error) {
	resolved := orktypes.ClusterRoleTemplateSource{
		Reconcile: src.Reconcile,
	}
	var err error

	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("clusterrole.name: %w", err)
	}
	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("clusterrole.sleep: %w", err)
	}
	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("clusterrole.labels: %w", err)
	}

	for _, rule := range src.Rules {
		resolvedRule := orktypes.PolicyRuleSpec{
			APIGroups: rule.APIGroups,
			Resources: rule.Resources,
			Verbs:     rule.Verbs,
		}
		for _, rn := range rule.ResourceNames {
			rv, resolveErr := r.Resolve(rn)
			if resolveErr != nil {
				return resolved, fmt.Errorf("clusterrole.rules.resourceNames: %w", resolveErr)
			}
			resolvedRule.ResourceNames = append(resolvedRule.ResourceNames, rv)
		}
		resolved.Rules = append(resolved.Rules, resolvedRule)
	}

	return resolved, nil
}

// ResolveClusterRoleBindingTemplate resolves all template expressions in a ClusterRoleBindingTemplateSource.
func (r *Resolver) ResolveClusterRoleBindingTemplate(src orktypes.ClusterRoleBindingTemplateSource) (orktypes.ClusterRoleBindingTemplateSource, error) {
	resolved := orktypes.ClusterRoleBindingTemplateSource{
		ForceConflict: src.ForceConflict,

		Reconcile: src.Reconcile,
	}
	var err error

	if resolved.Name, err = r.Resolve(src.Name); err != nil {
		return resolved, fmt.Errorf("clusterrolebinding.name: %w", err)
	}
	if resolved.Sleep, err = r.Resolve(src.Sleep); err != nil {
		return resolved, fmt.Errorf("clusterrolebinding.sleep: %w", err)
	}
	if resolved.Labels, err = r.ResolveMap(src.Labels); err != nil {
		return resolved, fmt.Errorf("clusterrolebinding.labels: %w", err)
	}

	resolved.RoleRef.Kind = src.RoleRef.Kind
	if resolved.RoleRef.Name, err = r.Resolve(src.RoleRef.Name); err != nil {
		return resolved, fmt.Errorf("clusterrolebinding.roleRef.name: %w", err)
	}

	for i, s := range src.Subjects {
		rs := orktypes.SubjectSpec{Kind: s.Kind}
		if rs.Name, err = r.Resolve(s.Name); err != nil {
			return resolved, fmt.Errorf("clusterrolebinding.subjects[%d].name: %w", i, err)
		}
		if rs.Namespace, err = r.Resolve(s.Namespace); err != nil {
			return resolved, fmt.Errorf("clusterrolebinding.subjects[%d].namespace: %w", i, err)
		}
		resolved.Subjects = append(resolved.Subjects, rs)
	}

	return resolved, nil
}
