// pkg/motif/expander.go
//
// Expander instantiates a Motif by binding its inputs and expanding
// its resource blocks into a concrete HookTemplates value.
//
// Two modes:
//
// Static (ork doctor init): inputs resolved from explicit with: bindings
// at generation time. The expanded resources are inlined into the generated
// Katalog. No runtime dependency on the Motif.
//
// Dynamic (Katalog runtime): inputs resolved at Katalog startup, before
// any reconcile. The Motif is loaded once and its templates compiled with
// the input bindings.
package motif

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"github.com/orkspace/orkestra/pkg/note"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"gopkg.in/yaml.v3"
)

// ExpandedMotif holds the result of expanding a motif.
type ExpandedMotif struct {
	// Name is the motif's metadata.name, used in conflict error messages.
	Name string
	// OnCreate contains resources from resources.onCreate: — merged into the CRD's OnCreate phase.
	OnCreate *orktypes.HookTemplates
	// OnReconcile contains resources from the flat resources: fields — merged into OnReconcile.
	OnReconcile *orktypes.HookTemplates
	Status      *orktypes.StatusConfig
	Admission   *orktypes.Admission
	// Notes carries user-defined notes declared in the motif.
	// Merged into the Katalog's NoteRegistry during expandKatalogImports.
	Notes orktypes.NoteRegistry
	// Profiles carries user-defined profiles declared in the motif.
	// Merged into the Katalog's ProfileRegistry during expandKatalogImports.
	Profiles orktypes.ProfileRegistry
}

// HasResources returns true when the motif produced any resource templates.
func (e *ExpandedMotif) HasResources() bool {
	return e.OnCreate != nil || e.OnReconcile != nil
}

// HasStatus reports whether the motif defines status fields or conditions.
func (e *ExpandedMotif) HasStatus() bool {
	return e.Status != nil
}

// HasAdmission reports whether the motif includes admission rules.
func (e *ExpandedMotif) HasAdmission() bool {
	return e.Admission != nil
}

// Expand instantiates a Motif with the given input bindings and returns
// the expanded resources, status, and admission configuration.
//
// bindings maps input name → resolved value. Required inputs missing from
// bindings are a validation error. Unknown inputs in bindings are also an error.
// Optional inputs not in bindings use their Motif-declared defaults.
//
// Expand replaces all `{{ .inputs.Name }}` and `{{ inputs.Name }}` expressions
// in the YAML of resources, status, and admission with the resolved binding values.
// Other template expressions (e.g., `{{ .children.* }}`) are left untouched
// and will be evaluated at runtime by the reconciler.
func Expand(m *orktypes.Motif, bindings map[string]string) (*ExpandedMotif, error) {
	if err := validateBindings(m, bindings); err != nil {
		return nil, err
	}

	resolved := resolveDefaults(m, bindings)

	// ---- Expand resources ----
	var onCreate, onReconcile *orktypes.HookTemplates
	if m.Resources != nil {
		resourceYAML, err := yaml.Marshal(m.Resources)
		if err != nil {
			return nil, fmt.Errorf("marshaling motif resources: %w", err)
		}
		rendered, err := renderInputs(string(resourceYAML), resolved, m.Inputs)
		if err != nil {
			return nil, fmt.Errorf("rendering motif %q resources: %w", m.Metadata.Name, err)
		}
		var mr orktypes.MotifResources
		if err := yaml.Unmarshal([]byte(rendered), &mr); err != nil {
			return nil, fmt.Errorf("parsing expanded motif %q resources: %w", m.Metadata.Name, err)
		}
		if mr.OnCreate != nil {
			filterExpandedResources(mr.OnCreate)
			onCreate = mr.OnCreate
		}
		filterExpandedResources(&mr.HookTemplates)
		inline := mr.HookTemplates
		if !inline.Empty() {
			onReconcile = &inline
		}
	}

	// ---- Expand status ----
	var status *orktypes.StatusConfig
	if m.Status != nil {
		statusYAML, err := yaml.Marshal(m.Status)
		if err != nil {
			return nil, fmt.Errorf("marshaling motif status: %w", err)
		}
		rendered, err := renderInputs(string(statusYAML), resolved, m.Inputs)
		if err != nil {
			return nil, fmt.Errorf("rendering motif %q status: %w", m.Metadata.Name, err)
		}
		var statusConfig orktypes.StatusConfig
		if err := yaml.Unmarshal([]byte(rendered), &statusConfig); err != nil {
			return nil, fmt.Errorf("parsing expanded motif %q status: %w", m.Metadata.Name, err)
		}
		status = &statusConfig
	}

	// ---- Expand admission (validation + mutation) ----
	var admission *orktypes.Admission
	if m.Admission != nil {
		admissionYAML, err := yaml.Marshal(m.Admission)
		if err != nil {
			return nil, fmt.Errorf("marshaling motif admission: %w", err)
		}
		rendered, err := renderInputs(string(admissionYAML), resolved, m.Inputs)
		if err != nil {
			return nil, fmt.Errorf("rendering motif %q admission: %w", m.Metadata.Name, err)
		}
		var adm orktypes.Admission
		if err := yaml.Unmarshal([]byte(rendered), &adm); err != nil {
			return nil, fmt.Errorf("parsing expanded motif %q admission: %w", m.Metadata.Name, err)
		}
		admission = &adm
	}

	return &ExpandedMotif{
		Name:        m.Metadata.Name,
		OnCreate:    onCreate,
		OnReconcile: onReconcile,
		Status:      status,
		Admission:   admission,
		Notes:       m.Notes,
		Profiles:    m.Profiles,
	}, nil
}

// validateBindings checks that all required inputs are provided and no
// unknown inputs are supplied.
func validateBindings(m *orktypes.Motif, bindings map[string]string) error {
	declared := make(map[string]*orktypes.MotifInput, len(m.Inputs))
	for i := range m.Inputs {
		declared[m.Inputs[i].Name] = &m.Inputs[i]
	}

	for _, input := range m.Inputs {
		if input.Required {
			if _, ok := bindings[input.Name]; !ok {
				return fmt.Errorf(
					"motif %q: required input %q not provided in with:\n"+
						"  Motif requires: %s\n"+
						"  Missing: %s",
					m.Metadata.Name, input.Name,
					strings.Join(requiredInputNames(m.Inputs), ", "),
					input.Name,
				)
			}
		}
	}

	for name := range bindings {
		if _, ok := declared[name]; !ok {
			return fmt.Errorf(
				"motif %q: unknown input %q in with: — declared inputs: %s",
				m.Metadata.Name, name,
				strings.Join(inputNames(m.Inputs), ", "),
			)
		}
	}

	return nil
}

// resolveDefaults returns the full input map with all declared inputs present.
// Optional inputs not in bindings use their declared default (which may be "").
// All inputs are always included so the preprocessor has a complete context for
// complex expressions like {{ .inputs.loaderImage | default .inputs.image }}.
func resolveDefaults(m *orktypes.Motif, bindings map[string]string) map[string]string {
	resolved := make(map[string]string, len(m.Inputs))
	for _, input := range m.Inputs {
		if val, ok := bindings[input.Name]; ok {
			resolved[input.Name] = val
		} else {
			resolved[input.Name] = input.Default // "" for inputs with no default
		}
	}
	return resolved
}

// inputsExprRe matches any {{ ... }} block that references the inputs map.
// Used in the second pass of renderInputs to catch complex piped expressions
// like {{ .inputs.loaderImage | default .inputs.image }} that simple string
// replacement cannot handle.
var inputsExprRe = regexp.MustCompile(`\{\{-?\s*[^}]*\binputs\b[^}]*-?\}\}`)

// This is a safe optimisation — note.Map() is a pure function that always
// returns the same map. The template engine does not modify the FuncMap
// after registration.
var orkNotes = note.Map()

// renderInputs is the motif preprocessor: it fully resolves all {{ .inputs.* }}
// expressions so that only runtime expressions ({{ .metadata.* }}, {{ .spec.* }},
// {{ .children.* }}, etc.) remain in the output YAML.
//
// Two passes:
//  1. Fast exact-match replacement for the simple {{ .inputs.KEY }} pattern.
//  2. Go template evaluation (using the orkNotes FuncMap) for any remaining
//     expressions that reference inputs — e.g. {{ .inputs.key | default .inputs.fallback }}.
//     Only blocks containing "inputs" are evaluated; all other template expressions
//     are left untouched for the runtime resolver.
func renderInputs(resourceYAML string, resolved map[string]string, inputs []orktypes.MotifInput) (string, error) {
	// Build a type index for post-substitution unquoting.
	inputTypes := make(map[string]string, len(inputs))
	for _, inp := range inputs {
		inputTypes[inp.Name] = strings.ToLower(inp.Type)
	}

	// Pass 1: exact-match simple patterns
	result := resourceYAML
	for key, val := range resolved {
		for _, pat := range []string{
			fmt.Sprintf("{{ .inputs.%s }}", key),
			fmt.Sprintf("{{ inputs.%s }}", key),
		} {
			result = strings.ReplaceAll(result, pat, val)
		}
	}

	// Pass 1b: strip YAML quotes around substituted scalar values for typed inputs.
	// YAML marshal quotes template expressions (e.g. '{{ .inputs.replicas }}'); after
	// pass 1 that becomes '1' — a YAML string — which Kubernetes rejects for integer fields.
	for key, val := range resolved {
		typ := inputTypes[key]
		if typ != "integer" && typ != "number" && typ != "bool" && typ != "boolean" {
			continue
		}
		for _, q := range []string{"'", `"`} {
			result = strings.ReplaceAll(result, q+val+q, val)
		}
	}

	// Pass 2: evaluate remaining complex input expressions with Go template.
	// Build an interface{} inputs map so | default and other pipeline funcs work.
	inputsData := make(map[string]interface{}, len(resolved))
	for k, v := range resolved {
		inputsData[k] = v
	}
	data := map[string]interface{}{"inputs": inputsData}

	var evalErr error
	result = inputsExprRe.ReplaceAllStringFunc(result, func(expr string) string {
		if evalErr != nil {
			return expr
		}
		tmpl, err := template.New("").
			Option("missingkey=zero").
			Funcs(orkNotes).Parse(expr)

		if err != nil {
			return expr // malformed — leave as-is
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return expr // leave failed expressions as-is for runtime
		}
		out := strings.TrimSpace(buf.String())
		return strings.ReplaceAll(out, "<no value>", "")
	})

	return result, evalErr
}

// ValidateMotifTemplates checks that all inputs.X references in the resource
// YAML correspond to declared input names. Returns a list of error strings.
func ValidateMotifTemplates(m *orktypes.Motif) []string {
	var errs []string
	declared := make(map[string]bool)
	for _, input := range m.Inputs {
		declared[input.Name] = true
	}

	resourceYAML, err := yaml.Marshal(m.Resources)
	if err != nil {
		return []string{"could not marshal resources for template validation"}
	}

	re := regexp.MustCompile(`\{\{\s*(?:index\s+)?\.?inputs\.?(\w+)`)
	matches := re.FindAllStringSubmatch(string(resourceYAML), -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		inputName := match[1]
		if !declared[inputName] {
			errs = append(errs, fmt.Sprintf(
				"template references inputs.%s but no input named %q is declared",
				inputName, inputName,
			))
		}
	}
	return errs
}

// isRuntimeCondition reports whether a condition cannot be evaluated at motif
// expansion time. A condition is runtime when its field or comparison value still
// contains a template expression ({{ }}) — meaning it references .spec.*, .metadata.*,
// .status.*, .external.*, or any other value only known during reconcile.
//
// After renderInputs runs, all {{ .inputs.* }} expressions have been replaced with
// their bound values. Any remaining {{ }} is a runtime expression that must be
// preserved on the resource for the reconciler to evaluate.
func isRuntimeCondition(cond orktypes.Condition) bool {
	if orktypes.IsTemplate(cond.Field) {
		return true
	}
	// Check comparison values
	_, val := orktypes.ResolveConditionOp(cond)
	return orktypes.IsTemplate(val)
}

// splitConditions partitions conditions into those that can be evaluated now
// (static — all inputs resolved, no remaining template expressions) and those
// that must be deferred to the reconciler (runtime — still contain {{ }}).
func splitConditions(conditions []orktypes.Condition) (static, runtime []orktypes.Condition) {
	for _, c := range conditions {
		if isRuntimeCondition(c) {
			runtime = append(runtime, c)
		} else {
			static = append(static, c)
		}
	}
	return
}

// evalMotifCondition evaluates a single motif condition against already-resolved values.
// The field is treated as a literal value (not a dot-notation path to look up),
// because inputs have already been substituted by renderInputs.
func evalMotifCondition(cond orktypes.Condition) bool {
	field := cond.Field

	// exists / notExists shorthands
	if cond.Exists != nil {
		return *cond.Exists == (field != "")
	}
	if cond.NotExists != nil {
		return *cond.NotExists == (field == "")
	}

	// operator or shorthand comparisons
	op, val := orktypes.ResolveConditionOp(cond)
	switch op {
	case orktypes.ConditionEquals:
		return field == val
	case orktypes.ConditionNotEquals:
		return field != val
	case orktypes.ConditionContains:
		return strings.Contains(field, val)
	case orktypes.ConditionPrefix:
		return strings.HasPrefix(field, val)
	case orktypes.ConditionSuffix:
		return strings.HasSuffix(field, val)
	}
	// Unknown or empty operator — treat as pass (don't silently drop resources)
	return true
}

// passesMotifConditions reports whether all conditions pass.
// Empty condition slice → unconditional (true).
func passesMotifConditions(conditions []orktypes.Condition, or []orktypes.Condition) bool {
	// AND conditions
	for _, c := range conditions {
		if !evalMotifCondition(c) {
			return false
		}
	}
	// or — if any pass, the block passes
	if len(or) > 0 {
		for _, c := range or {
			if evalMotifCondition(c) {
				return true
			}
		}
		return false
	}
	return true
}

// motifConditionFilter is the standard fn passed to HookTemplates.FilterResources.
// Static conditions (no {{ }}, already input-substituted) gate the resource at
// expansion time. Runtime conditions (still contain {{ }}) are preserved on the
// resource for the reconciler to evaluate against live CR state.
func motifConditionFilter(conditions, or []orktypes.Condition) (bool, []orktypes.Condition, []orktypes.Condition) {
	static, runtime := splitConditions(conditions)
	staticOr, runtimeOr := splitConditions(or)
	return passesMotifConditions(static, staticOr), runtime, runtimeOr
}

// filterExpandedResources applies motif condition filtering to ht using HookTemplates.FilterResources.
// Static conditions (no {{ }}, already input-substituted) gate inclusion at expansion time.
// Runtime conditions (still contain {{ }}) are preserved on the resource for the reconciler.
func filterExpandedResources(ht *orktypes.HookTemplates) {
	filtered := ht.FilterResources(motifConditionFilter)
	*ht = filtered
}

func inputNames(inputs []orktypes.MotifInput) []string {
	names := make([]string, len(inputs))
	for i, input := range inputs {
		names[i] = input.Name
	}
	return names
}

func requiredInputNames(inputs []orktypes.MotifInput) []string {
	var names []string
	for _, input := range inputs {
		if input.Required {
			names = append(names, input.Name)
		}
	}
	return names
}
