// pkg/migrate/generator.go
//
// Generates the Orkestra scaffolding files that accompany a migrated reconciler:
// katalog.yaml, simulate.yaml, e2e.yaml, and go.mod.
//
// All generated files are stubs with TODO markers. The user fills in
// CRD details (group, kind, location) and resource assertions.
package migrate

import (
	"fmt"
	"strings"
)

// Files holds all generated file contents keyed by filename.
type Files struct {
	Katalog    string
	Simulate   string
	E2E        string
	GoMod      string
	Makefile   string
	Dockerfile string
	README     string
}

// Options controls what the generator emits.
type Options struct {
	// ModulePath is the Go module path of the migrated operator (e.g. github.com/myorg/my-operator).
	// Used in go.mod and as a hint in katalog.yaml location fields.
	ModulePath string

	// OperatorName is the kebab-case name for the operator (e.g. webapp-operator).
	// Derived from ReceiverType if not set.
	OperatorName string

	// OrkVersion is the Orkestra CLI version (from pkg/version.Short()).
	// Written into go.mod as the orkestra require version.
	OrkVersion string
}

// Generate produces all scaffolding files from a Rewrite result.
func Generate(res *Result, opts Options) Files {
	if opts.OperatorName == "" {
		opts.OperatorName = toKebab(res.ReceiverType)
	}
	if opts.ModulePath == "" {
		opts.ModulePath = "github.com/myorg/" + opts.OperatorName
	}

	crdName := strings.TrimSuffix(opts.OperatorName, "-operator")
	crdName = strings.TrimSuffix(crdName, "-reconciler")
	constructorFn := "New" + res.ReceiverType

	return Files{
		Katalog:    generateKatalog(res, opts, crdName, constructorFn),
		Simulate:   generateSimulate(opts, crdName),
		E2E:        generateE2E(opts, crdName),
		GoMod:      generateGoMod(opts),
		Makefile:   generateMakefile(opts),
		Dockerfile: generateDockerfile(),
		README:     generateREADME(),
	}
}

func generateKatalog(res *Result, opts Options, crdName, constructorFn string) string {
	resources := buildResourcesBlock(res.Owns)
	watchBlock := buildWatchBlock(res.Watches)
	p := res.Primary

	kind := todoField(p.Kind, "set your CRD kind (e.g. WebApp)")
	version := p.Version
	if version == "" {
		version = "v1alpha1"
	}
	object := todoField(p.Object, "set the Go type name (e.g. WebApp)")
	objectList := p.ObjectList
	if objectList == "" {
		objectList = "TODO  # TODO(ork migrate): set the Go list type (e.g. WebAppList)"
	}
	location := p.Location
	if location == "" {
		location = opts.ModulePath + "/api/" + version + "  # TODO(ork migrate): adjust to your API types package"
	}
	alias := p.Alias
	if alias == "" {
		alias = "apiv1alpha1"
	}

	return fmt.Sprintf(`# Schema reference: https://orkestra.sh/docs/reference/schema/katalog/
apiVersion: orkestra.orkspace.io/v1
kind: Katalog
metadata:
  name: %s
  author: myorg  # TODO(ork migrate): set your name or org
  version: 0.1.0
  description: >
    Migrated from controller-runtime. The constructor lifted from %s
    runs inside Orkestra's reconcile loop — informer, workqueue, worker pool,
    leader election, and metrics are provided by the runtime.
  tags:
    - migration
    - constructor
    - typed

spec:
  crds:
    %s:
      apiTypes:
        group: TODO  # TODO(ork migrate): set your CRD group (e.g. apps.myorg.io)
        version: %s
        kind: %s
        plural: TODO # TODO(ork migrate): set the plural (e.g. webapps)
        object: %s
        objectList: %s
        location: %s
        alias: %s

      allowedNamespaces:
        - default

      operatorBox:
%s        reconciler:
          # default: false — the GenericReconciler is not used.
          # Your constructor owns the full reconcile loop.
          default: false

          constructor:
            location: %s/%s  # TODO(ork migrate): adjust to your reconciler package
            function: %s
            managedResources:
%s`, opts.OperatorName, res.PkgName, crdName,
		version, kind, object, objectList, location, alias,
		watchBlock, opts.ModulePath, res.PkgName, constructorFn, resources)
}

// todoField returns the value if non-empty, or a TODO placeholder with the given hint.
func todoField(value, hint string) string {
	if value != "" {
		return value
	}
	return "TODO  # TODO(ork migrate): " + hint
}

// buildResourcesBlock renders the managedResources: list under constructor: from Owns() detections.
func buildResourcesBlock(owns []DetectedType) string {
	if len(owns) == 0 {
		return "              - kind: TODO  # TODO(ork migrate): list every resource kind your operator manages\n"
	}
	var b strings.Builder
	for _, o := range owns {
		fmt.Fprintf(&b, "              - kind: %s\n", o.Kind)
		if o.APIVersion != "" && !strings.HasPrefix(o.APIVersion, "TODO") {
			fmt.Fprintf(&b, "                apiVersion: %s\n", o.APIVersion)
		} else if strings.HasPrefix(o.APIVersion, "TODO:") {
			fmt.Fprintf(&b, "                # TODO(ork migrate): apiVersion for %s (%s)\n",
				o.Kind, strings.TrimPrefix(o.APIVersion, "TODO: "))
		}
	}
	return b.String()
}

// buildWatchBlock renders the watch: block under operatorBox: from Watches() detections.
// Returns an empty string when no watch entries were detected.
func buildWatchBlock(watches []DetectedType) string {
	if len(watches) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("        watch:\n")
	for _, w := range watches {
		apiVer := w.APIVersion
		suffix := ""
		if strings.HasPrefix(apiVer, "TODO:") {
			// Emit the raw import path as a comment so the user knows where to look.
			suffix = "  # TODO(ork migrate): verify apiVersion (" + strings.TrimPrefix(apiVer, "TODO: ") + ")"
			apiVer = "TODO"
		}
		fmt.Fprintf(&b, "          - apiVersion: %s%s\n", apiVer, suffix)
		fmt.Fprintf(&b, "            kind: %s\n", w.Kind)
		fmt.Fprintf(&b, "            on: [create, update, delete]\n")
	}
	return b.String()
}

func generateSimulate(opts Options, crdName string) string {
	return fmt.Sprintf(`# Schema reference: https://orkestra.sh/docs/reference/schema/simulate/
apiVersion: orkestra.orkspace.io/v1
kind: Simulate
metadata:
  name: %s-sim
  description: >
    Verify resources are created in cycle 1 — no cluster needed.
    TODO(ork migrate): fill in the resource kinds your operator creates.

spec:
  katalog: ./katalog.yaml
  cr: ./cr.yaml
  cycles: 3

  expect:
    steady: true
    noErrors: true
    ops:
      - cycle: 1
        verb: create
        resource: TODO  # TODO(ork migrate): e.g. deployments, services, configmaps
      # - cycle: 1
      #   verb: create
      #   resource: services
`, opts.OperatorName)
}

func generateE2E(opts Options, crdName string) string {
	return fmt.Sprintf(`# Schema reference: https://orkestra.sh/docs/reference/schema/e2e/
apiVersion: orkestra.orkspace.io/v1
kind: E2E
metadata:
  name: %s-e2e
  description: >
    End-to-end test for %s.
    TODO(ork migrate): fill in your CRD kind, CR name, and assertions.

spec:
  katalog: ./katalog.yaml
  crd: ./crd.yaml
  cr: ./cr.yaml

  cluster:
    provider: kind
    name: ork-e2e
    reuse: false

  expect:
    - name: CR created
      after: cr-applied
      timeout: 30s
      resources:
        - kind: TODO  # TODO(ork migrate): set your CRD kind
          name: TODO  # TODO(ork migrate): set your CR name from cr.yaml
          namespace: default

    - name: Status written
      after: cr-applied
      timeout: 60s
      commands:
        - run: kubectl get TODO my-cr -o jsonpath='{.status.phase}'  # TODO(ork migrate): adjust
          outputContains: Running

    - name: Resources created
      after: cr-applied
      timeout: 90s
      resources:
        - kind: TODO  # TODO(ork migrate): e.g. Deployment
          name: TODO
          namespace: default
          ready: true

    - name: Cleanup verified
      after: cr-deleted
      timeout: 30s
      resources:
        - kind: TODO  # TODO(ork migrate): your managed resource
          name: TODO
          namespace: default
          count: 0
`, opts.OperatorName, opts.OperatorName)
}

func generateGoMod(opts Options) string {
	orkVer := opts.OrkVersion
	if orkVer == "" || orkVer == "dev" {
		orkVer = "v0.0.0  // TODO(ork migrate): replace with the published Orkestra version"
	}
	return fmt.Sprintf(`module %s

go 1.22

require (
	github.com/orkspace/orkestra %s
	k8s.io/api v0.29.3
	k8s.io/apimachinery v0.29.3
	k8s.io/client-go v0.29.3
)

// Run: go mod tidy
// to resolve all indirect dependencies.
`, opts.ModulePath, orkVer)
}

func generateMakefile(opts Options) string {
	return fmt.Sprintf(`# ── Typed Orkestra Operator ───────────────────────────────────────────────────
BINARY_NAME ?= ork
DEV_OUTPUT_DIR  ?= $(HOME)/.orkestra/bin
PROD_OUTPUT_DIR ?= $(HOME)/.orkestra/bin/runtime
KATALOG     ?= katalog.yaml

IMAGE_REPO  ?= myorg/%s
IMAGE_TAG   ?= latest
IMAGE       ?= $(IMAGE_REPO):$(IMAGE_TAG)

GOOS        ?= linux
GOARCH      ?= amd64
CGO_ENABLED  = 0

ORK_LDFLAGS := -X github.com/orkspace/orkestra/pkg/version.Version=$(GIT_VERSION) \
               -X github.com/orkspace/orkestra/pkg/version.Commit=$(GIT_COMMIT) \
               -X github.com/orkspace/orkestra/pkg/version.Date=$(GIT_DATE)

.PHONY: registry
registry:
	@[ -f go.mod.txt ] && mv go.mod.txt go.mod || true
	@[ -f go.sum.txt ] && mv go.sum.txt go.sum || true
	@find . -name "*.go" | xargs grep -l "^//go:build ignore$$" 2>/dev/null | while read f; do tail -n +3 "$$f" > "$$f.tmp" && mv "$$f.tmp" "$$f"; done || true
	ork generate registry --file $(KATALOG)

.PHONY: build
build:
	@[ -f go.mod.txt ] && mv go.mod.txt go.mod || true
	@[ -f go.sum.txt ] && mv go.sum.txt go.sum || true
	@find . -name "*.go" | xargs grep -l "^//go:build ignore$$" 2>/dev/null | while read f; do tail -n +3 "$$f" > "$$f.tmp" && mv "$$f.tmp" "$$f"; done || true
	@mkdir -p $(DEV_OUTPUT_DIR)
	go mod tidy
	gofmt -w .
	go build \
		-ldflags "$(ORK_LDFLAGS)" \
		-o $(DEV_OUTPUT_DIR)/$(BINARY_NAME) ./cmd/orkestra
	@echo "✅ Development build: $(DEV_OUTPUT_DIR)/$(BINARY_NAME)"

.PHONY: build-runtime
build-runtime:
	@[ -f go.mod.txt ] && mv go.mod.txt go.mod || true
	@[ -f go.sum.txt ] && mv go.sum.txt go.sum || true
	@find . -name "*.go" | xargs grep -l "^//go:build ignore$$" 2>/dev/null | while read f; do tail -n +3 "$$f" > "$$f.tmp" && mv "$$f.tmp" "$$f"; done || true
	@mkdir -p $(PROD_OUTPUT_DIR)
	go mod tidy
	gofmt -w .
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
		-tags "runtime" \
		-ldflags "$(ORK_LDFLAGS)" \
		-o $(PROD_OUTPUT_DIR)/$(BINARY_NAME) ./cmd/orkestra

.PHONY: validate
validate:
	$(DEV_OUTPUT_DIR)/$(BINARY_NAME) validate -f $(KATALOG)

.PHONY: e2e
e2e:
	$(DEV_OUTPUT_DIR)/$(BINARY_NAME) e2e

.PHONY: docker
docker:
	@cp $(PROD_OUTPUT_DIR)/$(BINARY_NAME) ./$(BINARY_NAME)
	docker build -t $(IMAGE) .
	@rm -f ./$(BINARY_NAME)
	@echo "✅ Docker image built: $(IMAGE)"

.PHONY: push
push:
	docker push $(IMAGE)

.PHONY: release
release: docker push

.PHONY: clean
clean:
	@rm -f $(DEV_OUTPUT_DIR)/$(BINARY_NAME)
	@rm -rf $(PROD_OUTPUT_DIR)

.PHONY: help
help:
	@echo "  registry       generate type registry from Katalog"
	@echo "  build          compile full development CLI"
	@echo "  build-runtime  compile production binary (only 'ork run')"
	@echo "  validate       run katalog validation"
	@echo "  e2e            run end-to-end tests"
	@echo "  docker         build-runtime + Docker image"
	@echo "  push           push Docker image"
	@echo "  release        docker + push"
	@echo "  clean          remove all local builds"
`, opts.OperatorName)
}

func generateDockerfile() string {
	return `FROM gcr.io/distroless/static-debian12:nonroot
COPY ork /usr/local/bin/ork
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/ork"]
`
}

func generateREADME() string {
	bt := "```"
	return `# Migration output

This directory was generated by ` + "`" + `ork migrate` + "`" + `. Your reconciler logic is preserved — the
constructor signature has been updated to match Orkestra's interface. Orkestra
now provides the informer, workqueue, worker pool, panic recovery, leader
election, and Prometheus metrics. You provide the reconcile logic.

To see a full worked example of the same migration path, run:

` + bt + `bash
ork init --pack from-controller-runtime
` + bt + `

---

## Step 1 — Resolve the TODOs

` + bt + `bash
grep -rn "TODO(ork migrate)" .
` + bt + `

Work through each marker in order:

- [ ] Update ` + "`" + `group` + "`" + `, ` + "`" + `kind` + "`" + `, ` + "`" + `plural` + "`" + `, ` + "`" + `location` + "`" + ` in ` + "`" + `katalog.yaml` + "`" + `
- [ ] Review ` + "`" + `managedResources:` + "`" + ` in ` + "`" + `katalog.yaml` + "`" + ` — add or correct the resource kinds your operator manages
- [ ] Fill in resource assertions in ` + "`" + `simulate.yaml` + "`" + ` and ` + "`" + `e2e.yaml` + "`" + `
- [ ] Delete ` + "`" + `main.go` + "`" + `, scheme registration, and manager setup

---

## Step 2 — Generate the type registry

` + bt + `bash
make registry
` + bt + `

Generates ` + "`" + `cmd/orkestra/main.go` + "`" + ` and ` + "`" + `pkg/typeregistry/zz_generated_typeregistry.go` + "`" + ` from ` + "`" + `katalog.yaml` + "`" + `.
Re-run whenever you change ` + "`" + `apiTypes` + "`" + ` fields.

---

## Step 3 — Build

` + bt + `bash
make build
` + bt + `

Builds a binary that includes your generated type registry and replaces ` + "`" + `~/.orkestra/bin/ork` + "`" + `.

---

## Step 4 — Validate

` + bt + `bash
make validate
` + bt + `

---

## Step 5 — Simulate

` + bt + `bash
make simulate
` + bt + `

Runs without a cluster. Fill in the resource assertions in ` + "`" + `simulate.yaml` + "`" + ` first.

---

## Step 6 — Run locally

` + bt + `bash
ork run --dev
` + bt + `

` + "`" + `--dev` + "`" + ` spins up a local kind cluster. Skip it if you already have a cluster running.
Apply your CRD and CR in a second terminal and watch the constructor fire.

---

## Step 7 — Observe in the Control Center

` + bt + `bash
ork control
` + bt + `

Open http://localhost:8081 to see health, consecutive failures, last error, and metrics per CRD.

---

## Step 8 — Release

Once the local run looks correct, build the production image and push:

` + bt + `bash
make release IMAGE=ghcr.io/myorg/my-operator:v0.1.0
` + bt + `

---

## Step 9 — Push the Katalog

` + bt + `bash
ork push .
ork inspect my-operator:v0.1.0
` + bt + `

---

## Step 10 — Generate bundle and deploy

` + bt + `bash
ork generate bundle -o bundle.yaml
kubectl apply -f bundle.yaml
` + bt + `

---

## What to know

**No changes to ` + "`" + `Reconcile` + "`" + `, struct fields, or call sites.**
The injected constructor calls ` + "`" + `orkadapter.ToClient(kube)` + "`" + ` to wrap the interface as
` + "`" + `client.Client` + "`" + ` — your existing field and all ` + "`" + `r.client.*` + "`" + ` calls compile unchanged.

**` + "`" + `ctrl.Result{RequeueAfter: X}` + "`" + ` is preserved.**
The bridge propagates ` + "`" + `RequeueAfter` + "`" + ` to Orkestra's work queue — no changes needed.

**` + "`" + `SetupWithManager` + "`" + `, ` + "`" + `main.go` + "`" + `, and scheme registration are gone.**
Orkestra owns the informer, workqueue, and manager. Delete them — do not
port them into the new module.`
}

// toKebab converts a PascalCase type name to kebab-case.
// "WebAppReconciler" → "webapp-reconciler"
func toKebab(s string) string {
	if s == "" {
		return "my-operator"
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('-')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}
