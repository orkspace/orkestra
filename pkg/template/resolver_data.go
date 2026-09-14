// pkg/template/resolver_data.go
//
// Resolver extensions — context injection methods and data access.
//
// Every With* method returns a NEW Resolver with the original unchanged.
// This is the immutable extension pattern: safe for concurrent use,
// no risk of one reconcile's context leaking into another's.
//
// Extension chain (built in this order during reconcileImpl):
//
//  1. NewResolver(ctx, obj)         — base: spec, status, metadata
//  2. resolver.WithChildren(map)    — adds .children.deployment.status.*
//  3. resolver.WithItem(val, as)    — adds .item / .<as> for forEach loops
//  4. resolver.WithExternal(map)    — adds .external.<name>.status / .body
//  5. resolver.WithCross(map)       — adds .cross.<kind>.status.*
//  6. resolver.WithMetrics(map)     — adds .metrics.queueDepth / .workers / .autoscaleActive …
//  7. resolver.WithPrevious(map)    — adds .previous.* (rollback path only)
//
// Each extension is a shallow copy of the previous resolver's data map
// with one new top-level key added. The template engine sees the full
// accumulated context at execution time.
package template

import (
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// ─────────────────────────────────────────────────────────────────────────────
// Data access
// ─────────────────────────────────────────────────────────────────────────────

// Data returns the resolver's internal object map.
//
// The map contains the full CR as seen by template expressions:
//
//	.spec.*      — all spec fields
//	.status.*    — current status (from informer cache)
//	.metadata.*  — name, namespace, labels, annotations, generation
//	.children.*  — child resources, after WithChildren is called
//	.external.*  — HTTP call results, after WithExternal is called
//	.cross.*     — cross-CRD observations, after WithCross is called
//	.metrics.*   — live operatorbox runtime metrics, after WithMetrics is called
//	.health.*   — live operatorbox runtime health metrics, after WithHealth is called
//	.item        — current forEach item, after WithItem is called
//	.previous.*  — last successfully reconciled spec, after WithPrevious is called (rollback path only)
//
// The returned map is the live internal map — do not mutate it.
// Used by:
//   - resolveStatusFields — condition evaluation on status.fields when: blocks
//   - runProviders        — condition evaluation on provider declaration when: blocks
//   - filterProviderDeclarations — same
//   - EvaluateConditions  — any code needing the full object context
func (r *Resolver) Data() map[string]interface{} {
	return r.data
}

// OwnerName returns the CR's metadata.name.
// Convenience accessor — avoids resolving "{{ .metadata.name }}" for callers
// that only need the owner name for labelling child resources.
func (r *Resolver) OwnerName() string {
	return r.ownerName
}

// OwnerNamespace returns the CR's metadata.namespace.
func (r *Resolver) OwnerNamespace() string {
	return r.ownerNamespace
}

// ─────────────────────────────────────────────────────────────────────────────
// WithItem — forEach context injection
// ─────────────────────────────────────────────────────────────────────────────

// WithItem returns a new Resolver with a forEach item injected into the
// template context. Used by expandForEach when iterating over a list field.
//
// The item is injected under two keys:
//   - "item"  — always available as {{ .item }} regardless of as
//   - <as>    — the name declared in forEach.as, e.g. "region", "namespace"
//
// Both resolve to the same value. "item" is the canonical accessor;
// <as> is the semantic name that makes the Katalog more readable:
//
//	forEach:
//	  field: spec.regions
//	  as: region
//
//	# In expressions, both work:
//	name: "{{ .metadata.name }}-{{ .item }}"
//	name: "{{ .metadata.name }}-{{ .region }}"
//
// index is the 0-based position in the list, available as {{ .index }}.
func (r *Resolver) WithItem(value interface{}, as string, index int) *Resolver {
	newData := r.shallowCopy()
	newData["item"] = value
	newData["index"] = index
	if as != "" && as != "item" {
		newData[as] = value
	}
	return r.copyWith(newData)
}

// WithItemAndValue is the map-forEach variant of WithItem.
// Used when the forEach field resolves to a map[string]interface{} instead of a list.
//
//	.item  / .<as> → the map key (string)
//	.value          → the map value (object or string) — access nested fields as .value.replicas
//	.index          → 0-based iteration order (keys sorted alphabetically)
func (r *Resolver) WithItemAndValue(key interface{}, value interface{}, as string, index int) *Resolver {
	newData := r.shallowCopy()
	newData["item"] = key
	newData["value"] = value
	newData["index"] = index
	if as != "" && as != "item" {
		newData[as] = key
	}
	return r.copyWith(newData)
}

// ─────────────────────────────────────────────────────────────────────────────
// WithExternal — HTTP call result injection
// ─────────────────────────────────────────────────────────────────────────────

// WithExternal returns a new Resolver with HTTP call results injected under
// the "external" key. Used after runExternal completes.
//
// Each call result is keyed by the call's name from the Katalog:
//
//	external:
//	  - name: healthCheck
//	    url: "{{ .spec.serviceUrl }}/health"
//
// Results accessible in subsequent template expressions and when: conditions:
//
//	{{ .external.healthCheck.status }}    → HTTP status code as string
//	{{ .external.healthCheck.body }}      → response body (first 4KB)
//	{{ .external.healthCheck.error }}     → error message if call failed
//
// Resource declarations that follow in runTemplateReconcile can gate on these:
//
//	when:
//	  - field: external.healthCheck.status
//	    equals: "200"
func (r *Resolver) WithExternal(results map[string]interface{}) *Resolver {
	if len(results) == 0 {
		return r
	}
	newData := r.shallowCopy()
	newData["external"] = results
	return r.copyWith(newData)
}

// ─────────────────────────────────────────────────────────────────────────────
// WithCross — cross-CRD observation injection
// ─────────────────────────────────────────────────────────────────────────────

// WithCross returns a new Resolver with cross-CRD observation data injected
// under the "cross" key. Used after ReadCross completes.
//
// Each observed CR is keyed by the "as" name from the cross: declaration:
//
//	cross:
//	  - crd: database
//	    selector:
//	      name: "{{ .metadata.name }}"
//	    as: database
//
// Results accessible in subsequent expressions and when: conditions:
//
//	{{ .cross.database.status.phase }}      → Database CR's status.phase
//	{{ .cross.database.status.endpoint }}   → Database CR's status.endpoint
//	{{ .cross.database.spec.storageGb }}    → Database CR's spec.storageGb
//	{{ .cross.database.found }}             → "true" if CR was found
//
// The cross data is read from the target CRD's informer cache — zero API
// server calls for same-binary CRDs. HTTP fallback for cross-binary/cluster.
func (r *Resolver) WithCross(data map[string]interface{}) *Resolver {
	if len(data) == 0 {
		return r
	}
	newData := r.shallowCopy()
	newData["cross"] = data
	return r.copyWith(newData)
}

// ─────────────────────────────────────────────────────────────────────────────
// WithEvents — Events result injection
// ─────────────────────────────────────────────────────────────────────────────

// WithEvents returns a new Resolver with Kubernetes Event observation data
// injected under the "events" key.
//
// Each observed Event is keyed by the "name" of the Event declaration:
//
//	events:
//	  databaseReady:
//	    reason: DatabaseReady
//	    type: Normal
//	    reportingController: database.orkestra.io
//
// Results are accessible in subsequent expressions and when: conditions:
//
//	{{ .events.databaseReady.reason }}
//	{{ .events.databaseReady.type }}
//	{{ .events.databaseReady.reportingController }}
//	{{ .events.databaseReady.regarding.name }}
//
// Event data is populated by the observe layer when an Event matches an
// EventEntry declaration.
func (r *Resolver) WithEvents(data map[string]interface{}) *Resolver {
	if len(data) == 0 {
		return r
	}

	newData := r.shallowCopy()
	newData["events"] = data
	return r.copyWith(newData)
}

// ─────────────────────────────────────────────────────────────────────────────
// WithGit — Git hook result injection
// ─────────────────────────────────────────────────────────────────────────────
//
// WithGit returns a new Resolver with Git hook results injected under
// the "git" key. Used after runGit completes.
//
// The Git block is a generic bag of fields produced by the Git hook.
// Typical fields:
//
//	.git.commit    — current HEAD commit hash
//	.git.changed   — "true" if commit changed since last reconcile
//	.git.path      — local working directory path
//	.git.error     — error message if the Git operation failed
//	.git.called    — "true" if the Git hook ran at least once
//
// Katalogs can gate behavior on these fields:
//
//	when:
//	  - field: git.changed
//	    equals: "true"
//
//	status:
//	  - path: lastCommit
//	    value: "{{ .git.commit }}"
func (r *Resolver) WithGit(data map[string]interface{}) *Resolver {
	if len(data) == 0 {
		return r
	}
	newData := r.shallowCopy()
	newData["git"] = data
	return r.copyWith(newData)
}

// ─────────────────────────────────────────────────────────────────────────────
// WithDocker — Docker hook result injection
// ─────────────────────────────────────────────────────────────────────────────
//
// WithDocker returns a new Resolver with Docker hook results injected under
// the "docker" key. Used after runDocker completes.
//
// The Docker block is a generic bag of fields produced by the Docker hook.
// Typical fields:
//
//	.docker.image           — fully qualified image reference (registry/repo:tag)
//	.docker.buildSucceeded  — "true" if build completed successfully
//	.docker.error           — error message if build/push failed
//	.docker.called          — "true" if the Docker hook ran at least once
//
// Katalogs can gate behavior and status on these fields:
//
//	when:
//	  - field: docker.buildSucceeded
//	    equals: "true"
//
//	status:
//	  - path: image
//	    value: "{{ .docker.image }}"
func (r *Resolver) WithDocker(data map[string]interface{}) *Resolver {
	if len(data) == 0 {
		return r
	}
	newData := r.shallowCopy()
	newData["docker"] = data
	return r.copyWith(newData)
}

// ─────────────────────────────────────────────────────────────────────────────
// WithSpecOverride — derived rollback context
// ─────────────────────────────────────────────────────────────────────────────

// WithSpecOverride returns a new Resolver with the spec key replaced by the
// provided map. Used by the rollBackOnError: true path to run derived rollback
// templates against the previous spec without requiring .previous.spec.* syntax.
//
// The caller's templates use {{ .spec.image }} as written — during a derived
// rollback run, that expression resolves to the previous spec's image value.
// All other keys (.metadata, .status, .external, .cross) remain current.
func (r *Resolver) WithSpecOverride(spec map[string]interface{}) *Resolver {
	newData := r.shallowCopy()
	newData["spec"] = spec
	return r.copyWith(newData)
}

// ─────────────────────────────────────────────────────────────────────────────
// WithPrevious — rollback context injection
// ─────────────────────────────────────────────────────────────────────────────

// WithPrevious returns a new Resolver with the previous spec injected under
// the "previous" key. Used by runRollback when applying onRollback templates.
//
// The previous spec is the last successfully reconciled spec, captured in the
// orkestra.orkspace.io/previous-spec annotation before each spec change.
//
// Templates in onRollback: declarations can reference:
//
//	{{ .previous.spec.image }}        — previous image
//	{{ .previous.spec.replicas }}     — previous replica count
//	{{ .previous.metadata.name }}     — CR name (same across generations)
//
// The previous map is injected as-is from the decoded annotation — it mirrors
// the CR's .spec.* structure exactly.
func (r *Resolver) WithPrevious(previous map[string]interface{}) *Resolver {
	if len(previous) == 0 {
		return r
	}
	newData := r.shallowCopy()
	newData["previous"] = previous
	return r.copyWith(newData)
}

// ─────────────────────────────────────────────────────────────────────────────
// WithMetrics — live operatorbox runtime metrics injection
// ─────────────────────────────────────────────────────────────────────────────

// WithMetrics returns a new Resolver with live operatorbox runtime metrics
// injected under the "metrics" key. Makes the following available in templates:
//
//	{{ .metrics.queueDepth }}             — current workqueue depth
//	{{ .metrics.workers }}                — current effective worker count
//	{{ .metrics.autoscaleActive }}        — "true"/"false" — override active
//	{{ .metrics.workersBusyPercent }}     — worker utilisation %
//	{{ .metrics.workersIdlePercent }}     — idle worker %
//	{{ .metrics.errorRatePercent }}       — reconcile error rate %
//	{{ .metrics.reconcileDurationP95Ms }} — P95 reconcile latency (ms)
//
// Useful in status.fields to surface live runtime state into CR status.
func (r *Resolver) WithMetrics(metrics map[string]interface{}) *Resolver {
	if len(metrics) == 0 {
		return r
	}
	newData := r.shallowCopy()
	newData["metrics"] = metrics
	return r.copyWith(newData)
}

// ─────────────────────────────────────────────────────────────────────────────
// WithHealth — live operatorbox runtime health injection
// ─────────────────────────────────────────────────────────────────────────────

// WithHealth returns a new Resolver with live operatorbox runtime health
// injected under the "health" key. Makes the following available in templates:
//
//	{{ .health.healthy }}                 — boolean: overall health
//	{{ .health.state }}                   — "healthy" / "degraded" / "error"
//	{{ .health.started }}                 — runtime started flag
//	{{ .health.pending }}                 — pending startup
//	{{ .health.startedAt }}               — RFC3339 timestamp
//	{{ .health.uptime }}                  — human‑readable uptime
//	{{ .health.queueDepth }}              — current queue depth
//	{{ .health.errorRate }}               — reconcile error rate
//	{{ .health.consecutiveFails }}        — consecutive reconcile failures
//	{{ .health.totalReconciles }}         — total reconciles since start
//	{{ .health.resourceCount }}           — number of managed CRs
//	{{ .health.lastError }}               — last reconcile error (string)
//	{{ .health.lastReconcile }}           — timestamp of last reconcile
//	{{ .health.hasUnhealthyDependencies }}— dependency health flag
//
// Useful in status.fields to surface live runtime health into CR status.
func (r *Resolver) WithHealth(health map[string]interface{}) *Resolver {
	if len(health) == 0 {
		return r
	}
	newData := r.shallowCopy()
	newData["health"] = health
	return r.copyWith(newData)
}

// ─────────────────────────────────────────────────────────────────────────────
// WithUniquenessChecker — operator: unique injection
// ─────────────────────────────────────────────────────────────────────────────

// WithUniquenessChecker returns a new Resolver with a live UniquenessChecker
// injected under the "_uniquenessChecker" key — the string must match
// pkg/types' uniquenessCheckerKey constant (same convention as _cronWindows,
// see when.go). Not template-visible; read internally by operator: unique in
// both validation.rules and when:/or: (see orktypes.UniquenessChecker).
// Only the reconciler calls this, so the operator is enforced at reconcile
// time only.
func (r *Resolver) WithUniquenessChecker(checker orktypes.UniquenessChecker) *Resolver {
	if checker == nil {
		return r
	}
	newData := r.shallowCopy()
	newData["_uniquenessChecker"] = checker
	return r.copyWith(newData)
}

// ─────────────────────────────────────────────────────────────────────────────
// shallowCopy — internal helper
// ─────────────────────────────────────────────────────────────────────────────

// shallowCopy creates a shallow copy of r.data for With* methods.
// Each With* method adds exactly one new top-level key — a shallow copy
// is sufficient because nested maps are not modified, only new keys are added.
func (r *Resolver) shallowCopy() map[string]interface{} {
	newData := make(map[string]interface{}, len(r.data)+1)
	for k, v := range r.data {
		newData[k] = v
	}
	return newData
}

// copyWith returns a new Resolver with newData as its data map and all other
// fields (mergedFuncs, profiles, ownerName, ownerNamespace) inherited from r.
// All With* methods must use this instead of &Resolver{...} so that user-defined
// notes and profiles are not silently dropped when the resolver is extended.
func (r *Resolver) copyWith(newData map[string]interface{}) *Resolver {
	return &Resolver{
		data:           newData,
		ownerName:      r.ownerName,
		ownerNamespace: r.ownerNamespace,
		profiles:       r.profiles,
		mergedFuncs:    r.mergedFuncs,
	}
}
