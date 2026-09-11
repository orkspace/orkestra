package types

import (
	"fmt"
	"path/filepath"
)

// ExpandStatusInclude resolves the status.include field by reading the
// referenced file, unmarshaling its "fields:" list, and prepending it to
// the inline fields. Inline fields append after included ones.
// The include path is resolved relative to baseDir. Cleared after expansion.
func ExpandStatusInclude(s *StatusConfig, baseDir string) error {
	if s == nil || s.Include == "" {
		return nil
	}
	path := s.Include
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	data, err := readLocal(path)
	if err != nil {
		return fmt.Errorf("reading status.include %q: %w", s.Include, err)
	}
	var f struct {
		Fields []StatusFieldSpec `yaml:"fields"`
	}
	if err := strictUnmarshal(data, &f); err != nil {
		return fmt.Errorf("parsing status.include %q: %w", s.Include, err)
	}
	s.Fields = append(f.Fields, s.Fields...)
	s.Include = ""
	return nil
}

// ExpandValidationInclude resolves the validation.include field by reading the
// referenced file, unmarshaling its "rules:" list, and prepending it to the
// inline rules. Inline rules append after included ones.
// The include path is resolved relative to baseDir. Cleared after expansion.
// Any additional external calls are also expanded if declared
func ExpandValidationInclude(v *ValidationConfig, baseDir string) error {
	if v == nil || v.Include == "" {
		return nil
	}
	path := v.Include
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	data, err := readLocal(path)
	if err != nil {
		return fmt.Errorf("reading validation.include %q: %w", v.Include, err)
	}
	var f struct {
		Rules []ValidationRule `yaml:"rules"`
	}
	if err := strictUnmarshal(data, &f); err != nil {
		return fmt.Errorf("parsing validation.include %q: %w", v.Include, err)
	}
	v.Rules = append(f.Rules, v.Rules...)
	v.Include = ""
	return nil
}

// ExpandMutationInclude resolves the mutation.include field by reading the
// referenced file, unmarshaling its "rules:" list, and prepending it to the
// inline rules. Inline rules append after included ones.
// The include path is resolved relative to baseDir. Cleared after expansion.
// The include path is resolved relative to baseDir. Cleared after expansion.
func ExpandMutationInclude(mu *MutationConfig, baseDir string) error {
	if mu == nil || mu.Include == "" {
		return nil
	}
	path := mu.Include
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	data, err := readLocal(path)
	if err != nil {
		return fmt.Errorf("reading mutation.include %q: %w", mu.Include, err)
	}
	var f struct {
		Rules []MutationRule `yaml:"rules"`
	}
	if err := strictUnmarshal(data, &f); err != nil {
		return fmt.Errorf("parsing mutation.include %q: %w", mu.Include, err)
	}
	mu.Rules = append(f.Rules, mu.Rules...)
	mu.Include = ""

	// Expand external calls
	if mu.HasExternalCall() {
		var err error
		mu.External, err = ExpandExternalCalls(mu.External, baseDir)
		if err != nil {
			return fmt.Errorf("admission.mutation.external: %w", err)
		}
	}
	return nil
}

// ExpandConversionInclude resolves the conversion.include field by reading the
// referenced file, unmarshaling its "rules:" list, and prepending it to the
// inline rules. Inline rules append after included ones.
// The include path is resolved relative to baseDir. Cleared after expansion.
func ExpandConversionInclude(co *CRDConversion, baseDir string) error {
	if co == nil || co.Include == "" {
		return nil
	}
	path := co.Include
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	data, err := readLocal(path)
	if err != nil {
		return fmt.Errorf("reading conversion.include %q: %w", co.Include, err)
	}
	var f struct {
		Paths []ConversionPath `yaml:"paths"`
	}
	if err := strictUnmarshal(data, &f); err != nil {
		return fmt.Errorf("parsing conversion.include %q: %w", co.Include, err)
	}
	co.Paths = append(f.Paths, co.Paths...)
	co.Include = ""
	return nil
}

// ExpandNotesInclude resolves the notes.include field by reading the referenced
// file, unmarshaling its "functions:" list, and prepending it to the inline
// functions. Inline functions append after included ones.
// The include path is resolved relative to baseDir. Cleared after expansion.
func ExpandNotesInclude(nr *NoteRegistry, baseDir string) error {
	if nr.Include == "" {
		return nil
	}
	path := nr.Include
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	data, err := readLocal(path)
	if err != nil {
		return fmt.Errorf("reading notes.include %q: %w", nr.Include, err)
	}
	var f struct {
		Functions []UserDefinedNote `yaml:"functions"`
	}
	if err := strictUnmarshal(data, &f); err != nil {
		return fmt.Errorf("parsing notes.include %q: %w", nr.Include, err)
	}
	nr.Functions = append(f.Functions, nr.Functions...)
	nr.Include = ""
	return nil
}

// ExpandProfileInclude resolves the profiles.include field by reading the
// referenced file, unmarshaling its "profiles:" block, and prepending each
// sub-list before the inline entries. Inline entries append after included ones.
// The include path is resolved relative to baseDir. Cleared after expansion.
func ExpandProfileInclude(r *ProfileRegistry, baseDir string) error {
	if r.Include == "" {
		return nil
	}
	path := r.Include
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	data, err := readLocal(path)
	if err != nil {
		return fmt.Errorf("reading profiles.include %q: %w", r.Include, err)
	}
	var f struct {
		Profiles ProfileRegistry `yaml:"profiles"`
	}
	if err := strictUnmarshal(data, &f); err != nil {
		return fmt.Errorf("parsing profiles.include %q: %w", r.Include, err)
	}
	p := f.Profiles
	r.NetworkPolicies = append(p.NetworkPolicies, r.NetworkPolicies...)
	r.ResourceQuotas = append(p.ResourceQuotas, r.ResourceQuotas...)
	r.LimitRanges = append(p.LimitRanges, r.LimitRanges...)
	r.HPA = append(p.HPA, r.HPA...)
	r.PDB = append(p.PDB, r.PDB...)
	r.RollingUpdate = append(p.RollingUpdate, r.RollingUpdate...)
	r.Reconciler = append(p.Reconciler, r.Reconciler...)
	r.Resources = append(p.Resources, r.Resources...)
	r.Probes = append(p.Probes, r.Probes...)
	r.ContainerSecurity = append(p.ContainerSecurity, r.ContainerSecurity...)
	r.PodSecurity = append(p.PodSecurity, r.PodSecurity...)
	r.Include = ""
	return nil
}

// ExpandWatchEntries resolves include entries in a []WatchEntry list.
// An entry with include: set is replaced in-place by the "watch:" list from the
// referenced file. Entries without include: are kept as-is.
// The include path is resolved relative to baseDir.
func ExpandWatchEntries(entries []WatchEntry, baseDir string) ([]WatchEntry, error) {
	var expanded []WatchEntry
	for _, entry := range entries {
		if entry.Include == "" {
			expanded = append(expanded, entry)
			continue
		}
		path := entry.Include
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		data, err := readLocal(path)
		if err != nil {
			return nil, fmt.Errorf("reading watch include %q: %w", entry.Include, err)
		}
		var f struct {
			Watch []WatchEntry `yaml:"watch"`
		}
		if err := strictUnmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("parsing watch include %q: %w", entry.Include, err)
		}
		expanded = append(expanded, f.Watch...)
	}
	return expanded, nil
}

// ExpandEventEntries resolves include entries in a []EventEntry list.
// An entry with include: set is replaced in-place by the "event:" list from the
// referenced file. Entries without include: are kept as-is.
// The include path is resolved relative to baseDir.
func ExpandEventEntries(entries []EventEntry, baseDir string) ([]EventEntry, error) {
	var expanded []EventEntry
	for _, entry := range entries {
		if entry.Include == "" {
			expanded = append(expanded, entry)
			continue
		}
		path := entry.Include
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		data, err := readLocal(path)
		if err != nil {
			return nil, fmt.Errorf("reading watch include %q: %w", entry.Include, err)
		}
		var f struct {
			Events []EventEntry `yaml:"events"`
		}
		if err := strictUnmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("parsing watch include %q: %w", entry.Include, err)
		}
		expanded = append(expanded, f.Events...)
	}
	return expanded, nil
}

// ExpandReconcilerInclude resolves the reconciler.include field by reading the
// referenced file, unmarshaling its "reconciler:" block, and merging it under
// the inline config. Inline fields take precedence. Cleared after expansion.
func ExpandReconcilerInclude(r *ReconcilerConfig, baseDir string) error {
	if r == nil || r.Include == "" {
		return nil
	}
	path := r.Include
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	data, err := readLocal(path)
	if err != nil {
		return fmt.Errorf("reading reconciler.include %q: %w", r.Include, err)
	}
	var f struct {
		Reconciler ReconcilerConfig `yaml:"reconciler"`
	}
	if err := strictUnmarshal(data, &f); err != nil {
		return fmt.Errorf("parsing reconciler.include %q: %w", r.Include, err)
	}
	inc := f.Reconciler
	if r.Default == nil && inc.Default != nil {
		r.Default = inc.Default
	}
	if r.Hooks == nil && inc.Hooks != nil {
		r.Hooks = inc.Hooks
	}
	if r.ConstructorDecl == nil && inc.ConstructorDecl != nil {
		r.ConstructorDecl = inc.ConstructorDecl
	}
	if r.Profile == "" && inc.Profile != "" {
		r.Profile = inc.Profile
	}
	if r.Workers == 0 && inc.Workers != 0 {
		r.Workers = inc.Workers
	}
	if r.Resync.Duration == 0 && inc.Resync.Duration != 0 {
		r.Resync = inc.Resync
	}
	if r.Queue.Empty() && !inc.Queue.Empty() {
		r.Queue = inc.Queue
	}
	if r.Requeue == nil && inc.Requeue != nil {
		r.Requeue = inc.Requeue
	}
	r.Include = ""
	return nil
}

// ExpandSimulateOpsIncludes resolves include entries in expect.Ops, expect.Absent,
// and each per-CRD sub-expect. An entry {include: ./path.yaml} is replaced
// in-place by the ops: list from the referenced file.
// The include path is resolved relative to baseDir.
func ExpandSimulateOpsIncludes(expect *SimulateExpect, baseDir string) error {
	if expect == nil {
		return nil
	}
	var err error
	if expect.Ops, err = expandOpRules(expect.Ops, baseDir); err != nil {
		return err
	}
	if expect.Absent, err = expandOpRules(expect.Absent, baseDir); err != nil {
		return fmt.Errorf("absent: %w", err)
	}
	for name, sub := range expect.CRDs {
		if sub == nil {
			continue
		}
		if sub.Ops, err = expandOpRules(sub.Ops, baseDir); err != nil {
			return fmt.Errorf("crds[%s]: %w", name, err)
		}
		if sub.Absent, err = expandOpRules(sub.Absent, baseDir); err != nil {
			return fmt.Errorf("crds[%s].absent: %w", name, err)
		}
	}
	return nil
}

func expandOpRules(rules []SimulateOpRule, baseDir string) ([]SimulateOpRule, error) {
	var expanded []SimulateOpRule
	for _, rule := range rules {
		if rule.Include == "" {
			expanded = append(expanded, rule)
			continue
		}
		path := rule.Include
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		data, err := readLocal(path)
		if err != nil {
			return nil, fmt.Errorf("reading ops include %q: %w", rule.Include, err)
		}
		var f struct {
			Ops []SimulateOpRule `yaml:"ops"`
		}
		if err := strictUnmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("parsing ops include %q: %w", rule.Include, err)
		}
		expanded = append(expanded, f.Ops...)
	}
	return expanded, nil
}

// ExpandServeInclude resolves the serve.include field by reading the referenced
// file, unmarshaling its "fields:", "labels:", and "annotations:" maps, and
// merging them under the inline equivalents. Inline entries take precedence.
// The include path is resolved relative to baseDir. Cleared after expansion.
func ExpandServeInclude(serve *ServeConfig, baseDir string) error {
	if serve == nil || serve.Include == "" {
		return nil
	}
	path := serve.Include
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	data, err := readLocal(path)
	if err != nil {
		return fmt.Errorf("reading serve.include %q: %w", serve.Include, err)
	}
	var f struct {
		Fields      map[string]ServeFieldConfig `yaml:"fields"`
		Labels      map[string]ServeFieldConfig `yaml:"labels"`
		Annotations map[string]ServeFieldConfig `yaml:"annotations"`
	}
	if err := strictUnmarshal(data, &f); err != nil {
		return fmt.Errorf("parsing serve.include %q: %w", serve.Include, err)
	}
	merged := make(map[string]ServeFieldConfig, len(f.Fields)+len(serve.Fields))
	for k, v := range f.Fields {
		merged[k] = v
	}
	for k, v := range serve.Fields {
		merged[k] = v
	}
	serve.Fields = merged
	serve.Labels = mergeServeFieldConfigs(f.Labels, serve.Labels)
	serve.Annotations = mergeServeFieldConfigs(f.Annotations, serve.Annotations)
	serve.Include = ""
	return nil
}

// ExpandGatewayAPIAuthInclude resolves include entries in GatewayConfig.API.Auth.
// If auth.Include is set, it reads the referenced file, unmarshals its "tokens:" list,
// and merges it with the inline tokens. Inline tokens override included tokens
// with the same name.
// The include path is resolved relative to baseDir. Cleared after expansion.
func ExpandGatewayAPIAuthInclude(gw *GatewayConfig, baseDir string) error {
	if gw == nil || gw.API == nil {
		return nil
	}
	auth := &gw.API.Auth
	if auth.Include == "" {
		return nil
	}

	path := auth.Include
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}

	data, err := readLocal(path)
	if err != nil {
		return fmt.Errorf("reading gateway.api.auth.include %q: %w", auth.Include, err)
	}

	var f struct {
		Tokens []APIToken `yaml:"tokens"`
	}
	if err := strictUnmarshal(data, &f); err != nil {
		return fmt.Errorf("parsing gateway.api.auth.include %q: %w", auth.Include, err)
	}

	// Merge: included tokens first, then inline overrides by name
	merged := make(map[string]APIToken)
	for _, t := range f.Tokens {
		merged[t.Name] = t
	}
	for _, t := range auth.Tokens {
		merged[t.Name] = t
	}

	// Convert map back to slice
	auth.Tokens = make([]APIToken, 0, len(merged))
	for _, t := range merged {
		auth.Tokens = append(auth.Tokens, t)
	}
	auth.Include = ""

	return nil
}

// ExpandServeTargetShorthand converts the scalar shorthand form of serve.target
// into the canonical map form before include expansion runs.
// "target: myapp" → "target: {myapp: {primary: true}}"
// No-op when the target is already in map form or not set.
func ExpandServeTargetShorthand(serve *ServeConfig) error {
	if serve == nil || serve.Target.Shorthand == "" {
		return nil
	}
	name := serve.Target.Shorthand
	serve.Target.Entries = map[string]*ServeTargetConfig{
		name: {Primary: true},
	}
	serve.Target.Shorthand = ""
	return nil
}

// ExpandServeTargetIncludes resolves the include field on every entry in serve.target.
// For each entry with include set, the referenced file is read and unmarshaled
// into a ServeTargetConfig; inline fields (tokens, config) take precedence.
// The include field is cleared after expansion.
// The include path is resolved relative to baseDir.
func ExpandServeTargetIncludes(serve *ServeConfig, baseDir string) error {
	if serve == nil || len(serve.Target.Entries) == 0 {
		return nil
	}
	for name, entry := range serve.Target.Entries {
		if entry == nil || entry.Include == "" {
			continue
		}
		path := entry.Include
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		data, err := readLocal(path)
		if err != nil {
			return fmt.Errorf("reading serve.target[%q].include %q: %w", name, entry.Include, err)
		}
		var f ServeTargetConfig
		if err := strictUnmarshal(data, &f); err != nil {
			return fmt.Errorf("parsing serve.target[%q].include %q: %w", name, entry.Include, err)
		}
		if len(f.Tokens) > 0 && len(entry.Tokens) == 0 {
			entry.Tokens = f.Tokens
		}
		if f.Config != nil && entry.Config == nil {
			entry.Config = f.Config
		}
		entry.Include = ""
		serve.Target.Entries[name] = entry
	}
	return nil
}

// mergeServeFieldConfigs merges included and inline ServeFieldConfig maps, inline
// taking precedence. Returns nil when both inputs are empty.
func mergeServeFieldConfigs(included, inline map[string]ServeFieldConfig) map[string]ServeFieldConfig {
	if len(included) == 0 && len(inline) == 0 {
		return nil
	}
	merged := make(map[string]ServeFieldConfig, len(included)+len(inline))
	for k, v := range included {
		merged[k] = v
	}
	for k, v := range inline {
		merged[k] = v
	}
	return merged
}
