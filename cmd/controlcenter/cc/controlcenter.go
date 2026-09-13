package controlcenter

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	ccversion "github.com/orkspace/orkestra-cc/version"
)

//go:embed assets/templates/*.html assets/static/* assets/static/css/* assets/static/js/*
var Assets embed.FS

const TemplateDir = "assets/templates"

// ─────────────────────────────────────────────────────────────────────────────
// Config
// ─────────────────────────────────────────────────────────────────────────────

type Config struct {
	RefreshInterval      time.Duration
	LogLevel             string
	Version              string
	EnableRuntimeManager bool
	NoLogin              bool
	GatewayToken         string
}

// ─────────────────────────────────────────────────────────────────────────────
// Instance — one connected Orkestra runtime
// ─────────────────────────────────────────────────────────────────────────────
type Instance struct {
	URL             string
	Client          *Client
	Katalog         *KatalogResponse
	Healthy         bool
	LastError       string
	LastCheck       time.Time
	Status          string // "online", "starting" or "degraded"
	GatewayEndpoint string // from runtime /katalog; empty when no gateway
}

// ─────────────────────────────────────────────────────────────────────────────
// ControlCenter
// ─────────────────────────────────────────────────────────────────────────────

type ControlCenter struct {
	urls         []string
	instances    map[string]*Instance // keyed by URL
	mu           sync.RWMutex
	config       Config
	ready        atomic.Bool
	subscribers  sync.Map // map[chan struct{}]struct{}
	gatewayToken string
}

// New creates and starts a ControlCenter. Background fetches begin immediately.
func New(urls []string, config Config) *ControlCenter {
	// Load previously stored URLs
	stored, _ := LoadRuntimeStorage()
	allURLs := make(map[string]bool)
	for _, u := range urls {
		allURLs[normalizeURL(u)] = true
	}
	if stored != nil {
		for _, u := range stored.URLs {
			allURLs[normalizeURL(u)] = true
		}
	}
	finalURLs := make([]string, 0, len(allURLs))
	for u := range allURLs {
		finalURLs = append(finalURLs, u)
	}

	instances := make(map[string]*Instance)
	for _, url := range finalURLs {
		instances[url] = &Instance{
			URL:    url,
			Client: NewClient(url, config.RefreshInterval, config.LogLevel),
		}
	}
	cc := &ControlCenter{
		urls:         finalURLs,
		instances:    instances,
		config:       config,
		gatewayToken: config.GatewayToken,
	}
	go cc.backgroundFetchLoop()
	return cc
}

func (cc *ControlCenter) IsReady() bool { return cc.ready.Load() }
func (cc *ControlCenter) NoLogin() bool { return cc.config.NoLogin }

// ─────────────────────────────────────────────────────────────────────────────
// Background fetch
// ─────────────────────────────────────────────────────────────────────────────

func (cc *ControlCenter) backgroundFetchLoop() {
	cc.fetchAllKatalogs()
	t := time.NewTicker(cc.config.RefreshInterval)
	defer t.Stop()
	for range t.C {
		cc.fetchAllKatalogs()
	}
}

func (cc *ControlCenter) fetchAllKatalogs() {
	// Snapshot instance URLs without holding the lock during network I/O.
	// Holding a write lock across HTTP calls blocks all concurrent reads
	// (CRD detail pages, snapshot API) for the full fetch duration, causing
	// intermittent "pending / 0 reconciles" displays even when the runtime is healthy.
	cc.mu.RLock()
	urls := make([]string, 0, len(cc.instances))
	for u := range cc.instances {
		urls = append(urls, u)
	}
	cc.mu.RUnlock()

	anyOK := false

	for _, u := range urls {
		cc.mu.RLock()
		inst, ok := cc.instances[u]
		cc.mu.RUnlock()
		if !ok {
			continue // instance was removed between snapshot and now
		}

		now := time.Now()
		kat, err := inst.Client.FetchKatalog()

		// Update only this instance under a brief write lock
		cc.mu.Lock()
		if inst, ok = cc.instances[u]; ok {
			inst.LastCheck = now
			if err != nil {
				inst.Katalog = nil
				inst.Status = "offline"
				inst.Healthy = false
				inst.LastError = err.Error()
				log.Printf("WARN: fetch katalog from %s: %v", u, err)
			} else {
				inst.GatewayEndpoint = kat.GatewayEndpoint
				if kat.IsKonductor {
					// Leader pod — data is authoritative, always update.
					inst.Katalog = kat
					inst.Status = "online"
					inst.Healthy = true
					inst.LastError = ""
				} else if inst.Katalog == nil {
					// Follower pod but we have nothing yet — accept it so the
					// katalog appears immediately rather than waiting for a lucky
					// tick that hits the leader. It may show CRDs as pending until
					// the next leader response overwrites it.
					inst.Katalog = kat
					inst.Status = "starting"
					inst.Healthy = false
					inst.LastError = ""
				}
				// Follower + already have good data → keep last known-good snapshot,
				// discard this response so the display does not flip.
				log.Printf("INFO: fetched katalog %q from %s (%d CRDs, gateway=%q)",
					kat.Name, u, len(kat.CRDs), kat.GatewayEndpoint)
			}
		}
		cc.mu.Unlock()

		if err == nil {
			anyOK = true
		}
	}

	cc.ready.Store(anyOK)
	cc.notifySubscribers()
}

// notifySubscribers sends a signal to all connected SSE clients.
func (cc *ControlCenter) notifySubscribers() {
	cc.subscribers.Range(func(key, _ interface{}) bool {
		ch := key.(chan struct{})
		select {
		case ch <- struct{}{}:
		default:
		}
		return true
	})
}

// ServeSSE handles Server-Sent Events for live page reloads.
func (cc *ControlCenter) ServeSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan struct{}, 1)
	cc.subscribers.Store(ch, struct{}{})
	defer cc.subscribers.Delete(ch)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Send initial connection confirmation
	fmt.Fprintf(w, "data: connected\n\n")
	flusher.Flush()

	for {
		select {
		case <-ch:
			fmt.Fprintf(w, "data: update\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Instance lookups
// ─────────────────────────────────────────────────────────────────────────────

// instanceByKatalogName returns the instance whose loaded katalog has the given name.
func (cc *ControlCenter) instanceByKatalogName(name string) (*Instance, bool) {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	for _, inst := range cc.instances {
		if inst.Katalog != nil && inst.Katalog.Name == name {
			return inst, true
		}
	}
	return nil, false
}

// clientFor returns the Client for an instance URL.
// Falls back to the first available client if the URL is not found.
func (cc *ControlCenter) clientFor(instanceURL string) *Client {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	if inst, ok := cc.instances[instanceURL]; ok {
		return inst.Client
	}
	// URL not found — return first available client as fallback
	for _, inst := range cc.instances {
		return inst.Client
	}
	return NewClient(instanceURL, cc.config.RefreshInterval, cc.config.LogLevel)
}

// ─────────────────────────────────────────────────────────────────────────────
// Router
// ─────────────────────────────────────────────────────────────────────────────

// ServeHTTP dispatches all /controlcenter/** requests.
//
// Route table (after stripping /controlcenter prefix):
//
//	/                                                   → index
//	/assets/**                                          → static files
//	/metrics                                            → metrics page
//	/debug/file                                         → debug
//	/docs                                               → docs landing
//	/docs/{katalog}/{crd}                               → CRD docs page
//	/katalog/{katalog}                                  → katalog panel
//	/katalog/{katalog}/crd/{crd}                        → CRD detail
//	/katalog/{katalog}/crd/{crd}/cr                     → CR list
//	/katalog/{katalog}/crd/{crd}/cr/{name}              → CR detail (cluster-scoped)
//	/katalog/{katalog}/crd/{crd}/cr/{ns}/{name}         → CR detail (namespaced)
func (cc *ControlCenter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/controlcenter")
	if path == "" || path == "/" {
		cc.handleIndex(w, r)
		return
	}

	log.Printf("DEBUG: %s %s", r.Method, path)

	switch {
	case strings.HasPrefix(path, "/assets/"):
		cc.serveAsset(w, r, strings.TrimPrefix(path, "/assets/"))

	case path == "/sse":
		cc.ServeSSE(w, r)

	case path == "/api/snapshot":
		cc.handleAPISnapshot(w, r)

	case path == "/metrics":
		cc.handleMetricsPage(w, r)

	case path == "/debug/file":
		cc.handleDebugFile(w, r)

	case path == "/docs":
		cc.handleDocsLanding(w, r)

	case strings.HasPrefix(path, "/docs/"):
		// /docs/{katalog}           → developer docs or operator CRD list
		// /docs/{katalog}/{crd}     → CRD docs page
		parts := strings.SplitN(strings.TrimPrefix(path, "/docs/"), "/", 2)
		if len(parts) == 2 {
			cc.handleCRDDocs(w, r, parts[0], parts[1])
		} else if len(parts) == 1 && parts[0] != "" {
			cc.handleKatalogDocs(w, r, parts[0])
		} else {
			cc.handleDocsLanding(w, r)
		}

	case path == "/api/idp/apply" && r.Method == http.MethodPost:
		cc.handleIDPApply(w, r)

	case strings.HasPrefix(path, "/api/idp/schema/"):
		target := strings.TrimPrefix(path, "/api/idp/schema/")
		cc.handleIDPSchema(w, r, target)

	case strings.HasPrefix(path, "/katalog/"):
		// Strip leading slash and split
		parts := strings.Split(strings.Trim(path, "/"), "/")
		cc.routeKatalog(w, r, parts)
	case path == "/api/instances" && r.Method == http.MethodGet:
		cc.handleListInstances(w, r)
	case path == "/api/instances" && r.Method == http.MethodPost:
		cc.handleAddInstance(w, r)
	case strings.HasPrefix(path, "/api/instances/") && r.Method == http.MethodPut:
		cc.handleUpdateInstance(w, r)
	case strings.HasPrefix(path, "/api/instances/") && r.Method == http.MethodDelete:
		cc.handleDeleteInstance(w, r)
	default:
		cc.handleNotFound(w, r)
	}
}

// routeKatalog dispatches /katalog/** paths.
// parts has no leading slash: ["katalog", "{name}", ...]
func (cc *ControlCenter) routeKatalog(w http.ResponseWriter, r *http.Request, parts []string) {
	// parts[0] == "katalog"
	if len(parts) < 2 {
		cc.handleNotFound(w, r)
		return
	}

	katalogName := parts[1]

	switch {
	// /katalog/{name}/app/{appName}  — developer path
	case len(parts) == 4 && parts[2] == "app":
		cc.handleDevAppDetail(w, r, katalogName, parts[3])

	// /katalog/{name}/crd/{crd}/cr[/...]
	case len(parts) >= 5 && parts[2] == "crd" && parts[4] == "cr":
		crdName := parts[3]
		inst, ok := cc.instanceByKatalogName(katalogName)
		if !ok {
			cc.handleNotFound(w, r)
			return
		}
		cc.routeCR(w, r, inst.URL, inst.Katalog.Name, crdName, parts[4:])

	// /katalog/{name}/crd/{crd}/raw  or  /katalog/{name}/crd/{crd}/enriched
	case len(parts) == 5 && parts[2] == "crd" && (parts[4] == "raw" || parts[4] == "enriched"):
		cc.handleProxyCRDSpec(w, r, katalogName, parts[3], parts[4])

	// /katalog/{name}/crd/{crd}
	case len(parts) >= 4 && parts[2] == "crd":
		cc.handleCRDDetail(w, r, katalogName, parts[3])

	// /katalog/{name}/raw  or  /katalog/{name}/enriched
	case len(parts) == 3 && (parts[2] == "raw" || parts[2] == "enriched"):
		cc.handleProxyKatalogSpec(w, r, katalogName, parts[2])

	// /katalog/{name}
	default:
		cc.handleKatalogPanel(w, r, katalogName)
	}
}

// routeCR dispatches CR sub-paths.
// crParts: ["cr"], ["cr", "{name}"], ["cr", "{ns}", "{name}"]
func (cc *ControlCenter) routeCR(w http.ResponseWriter, r *http.Request, instanceURL, katalogName, crdName string, crParts []string) {
	// crParts[0] == "cr"
	switch len(crParts) {
	case 1:
		// /cr → list
		cc.handleCRList(w, r, instanceURL, katalogName, crdName)
	case 2:
		if crParts[1] == "create" {
			cc.handleIDPCreateForm(w, r, instanceURL, katalogName, crdName)
			return
		}
		// /cr/{name} → cluster-scoped detail
		cc.handleCRDetail(w, r, instanceURL, katalogName, crdName, "", crParts[1])
	case 3:
		// /cr/{ns}/{name} → namespaced detail
		cc.handleCRDetail(w, r, instanceURL, katalogName, crdName, crParts[1], crParts[2])
	default:
		cc.handleNotFound(w, r)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Raw / Enriched proxy handlers
// ─────────────────────────────────────────────────────────────────────────────

// handleProxyCRDSpec proxies GET /katalog/{crd}/raw or /katalog/{crd}/enriched
// from the Orkestra runtime and returns the JSON body to the browser.
func (cc *ControlCenter) handleProxyCRDSpec(w http.ResponseWriter, r *http.Request, katalogName, crdName, kind string) {
	inst, ok := cc.instanceByKatalogName(katalogName)
	if !ok {
		http.Error(w, "katalog not found", http.StatusNotFound)
		return
	}
	target := inst.URL + "/katalog/" + crdName + "/" + kind
	cc.proxyJSON(w, target)
}

// handleProxyKatalogSpec proxies GET /katalog/raw or /katalog/enriched
// from the Orkestra runtime and returns the JSON body to the browser.
func (cc *ControlCenter) handleProxyKatalogSpec(w http.ResponseWriter, r *http.Request, katalogName, kind string) {
	inst, ok := cc.instanceByKatalogName(katalogName)
	if !ok {
		http.Error(w, "katalog not found", http.StatusNotFound)
		return
	}
	target := inst.URL + "/katalog/" + kind
	cc.proxyJSON(w, target)
}

// proxyJSON fetches target and pipes the JSON response body to w.
func (cc *ControlCenter) proxyJSON(w http.ResponseWriter, target string) {
	resp, err := http.Get(target) //nolint:gosec
	if err != nil {
		http.Error(w, "upstream error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body) //nolint:errcheck
}

// ─────────────────────────────────────────────────────────────────────────────
// Page handlers
// ─────────────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────────────
// Live snapshot API — used by JS for partial DOM updates (no full-page reload)
// ─────────────────────────────────────────────────────────────────────────────

type StatsSnap struct {
	TotalKatalogs   int `json:"totalKatalogs"`
	HealthyKatalogs int `json:"healthyKatalogs"`
	TotalCRDs       int `json:"totalCRDs"`
	TotalWorkers    int `json:"totalWorkers"`
	TotalResources  int `json:"totalResources"`
}

type KatalogSnap struct {
	Name           string `json:"name"`
	Healthy        bool   `json:"healthy"`
	TotalCRDs      int    `json:"totalCRDs"`
	HealthyCRDs    int    `json:"healthyCRDs"`
	TotalWorkers   int    `json:"totalWorkers"`
	TotalResources int    `json:"totalResources"`
}

type SnapshotData struct {
	Stats    StatsSnap     `json:"stats"`
	Katalogs []KatalogSnap `json:"katalogs"`
	Updated  string        `json:"updated"`
}

func (cc *ControlCenter) handleAPISnapshot(w http.ResponseWriter, _ *http.Request) {
	cc.mu.RLock()
	insts := make([]*Instance, 0, len(cc.instances))
	for _, inst := range cc.instances {
		if inst.Katalog != nil {
			insts = append(insts, inst)
		}
	}
	cc.mu.RUnlock()

	sort.Slice(insts, func(i, j int) bool {
		return insts[i].Katalog.Name < insts[j].Katalog.Name
	})

	stats := StatsSnap{}
	var katalogs []KatalogSnap

	for _, inst := range insts {
		kat := inst.Katalog
		healthyCRDs := 0
		for _, crd := range kat.CRDs {
			if crd.Healthy {
				healthyCRDs++
			}
		}
		stats.TotalKatalogs++
		stats.TotalCRDs += len(kat.CRDs)
		stats.TotalWorkers += sumWorkers(kat.CRDs)
		stats.TotalResources += sumResources(kat.CRDs)
		if kat.Healthy {
			stats.HealthyKatalogs++
		}
		katalogs = append(katalogs, KatalogSnap{
			Name:           kat.Name,
			Healthy:        kat.Healthy,
			TotalCRDs:      len(kat.CRDs),
			HealthyCRDs:    healthyCRDs,
			TotalWorkers:   sumWorkers(kat.CRDs),
			TotalResources: sumResources(kat.CRDs),
		})
	}

	if katalogs == nil {
		katalogs = []KatalogSnap{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(SnapshotData{
		Stats:    stats,
		Katalogs: katalogs,
		Updated:  time.Now().UTC().Format(time.RFC3339),
	})
}

func (cc *ControlCenter) handleIndex(w http.ResponseWriter, _ *http.Request) {
	cc.mu.RLock()
	insts := make([]*Instance, 0, len(cc.instances))
	for _, inst := range cc.instances {
		if inst.Katalog != nil {
			insts = append(insts, inst)
		}
	}
	cc.mu.RUnlock()

	sort.Slice(insts, func(i, j int) bool {
		return insts[i].Katalog.Name < insts[j].Katalog.Name
	})

	var summaries []KatalogSummary
	totalCRDs, totalWorkers, totalResources, totalApps, healthyKatalogs := 0, 0, 0, 0, 0
	hasOperatorKatalogs := false
	clusterSeen := map[string]struct{}{}
	nsSeen := map[string]struct{}{}

	for _, inst := range insts {
		kat := inst.Katalog
		healthyCRDs := 0
		for _, crd := range kat.CRDs {
			if crd.Healthy {
				healthyCRDs++
			}
		}

		// Collect distinct namespaces from this katalog's namespace grouping.
		var nsKeys []string
		for ns := range kat.Namespaces {
			nsKeys = append(nsKeys, ns)
			nsSeen[ns] = struct{}{}
		}
		sort.Strings(nsKeys)

		if kat.ClusterName != "" {
			clusterSeen[kat.ClusterName] = struct{}{}
		}

		summary := KatalogSummary{
			Name:             kat.Name,
			Description:      kat.Description,
			Version:          kat.Version,
			Healthy:          kat.Healthy,
			CreatedBy:        kat.CreatedBy,
			AppCount:         len(kat.Projects),
			TotalCRDs:        len(kat.CRDs),
			HealthyCRDs:      healthyCRDs,
			TotalWorkers:     sumWorkers(kat.CRDs),
			TotalResources:   sumResources(kat.CRDs),
			ClusterName:      kat.ClusterName,
			Namespaces:       nsKeys,
			NamespaceDetails: kat.Namespaces,
		}
		summaries = append(summaries, summary)
		if kat.CreatedBy == "orkdoctor" {
			totalApps += len(kat.Projects)
		} else {
			hasOperatorKatalogs = true
			totalCRDs += len(kat.CRDs)
			totalWorkers += sumWorkers(kat.CRDs)
			totalResources += sumResources(kat.CRDs)
		}
		if kat.Healthy {
			healthyKatalogs++
		}
	}

	allClusters := make([]string, 0, len(clusterSeen))
	for c := range clusterSeen {
		allClusters = append(allClusters, c)
	}
	sort.Strings(allClusters)

	allNamespaces := make([]string, 0, len(nsSeen))
	for ns := range nsSeen {
		allNamespaces = append(allNamespaces, ns)
	}
	sort.Strings(allNamespaces)

	cc.renderTemplate(w, "index.html", IndexData{
		Katalogs:             summaries,
		TotalKatalogs:        len(summaries),
		HealthyKatalogs:      healthyKatalogs,
		TotalApps:            totalApps,
		TotalCRDs:            totalCRDs,
		TotalWorkers:         totalWorkers,
		TotalResources:       totalResources,
		AnyHealthy:           len(summaries) > 0,
		HasOperatorKatalogs:  hasOperatorKatalogs,
		OrkestraURLs:         strings.Join(cc.urls, ", "),
		CCVersion:            ccversion.Short(),
		AllClusters:          allClusters,
		AllNamespaces:        allNamespaces,
		EnableRuntimeManager: cc.config.EnableRuntimeManager,
	})
}

func (cc *ControlCenter) handleKatalogPanel(w http.ResponseWriter, r *http.Request, katalogName string) {
	inst, ok := cc.instanceByKatalogName(katalogName)
	if !ok {
		// Try a fresh fetch once before giving up
		cc.fetchAllKatalogs()
		inst, ok = cc.instanceByKatalogName(katalogName)
		if !ok {
			cc.handleNotFound(w, r)
			return
		}
	}

	kat := inst.Katalog

	// Developer path: render a simplified app-focused view.
	if kat.CreatedBy == "orkdoctor" {
		cc.renderDevApps(w, r, kat)
		return
	}

	ns := r.URL.Query().Get("ns")

	// Sort CRDs by name for consistent display
	sortedCRDs := make([]CRDSummary, len(kat.CRDs))
	copy(sortedCRDs, kat.CRDs)
	sort.Slice(sortedCRDs, func(i, j int) bool {
		return sortedCRDs[i].Name < sortedCRDs[j].Name
	})

	// When viewing a specific namespace, use that namespace's description and
	// version (set from each CRD's source Katalog metadata) rather than the
	// top-level Komposer description which belongs to the whole bundle.
	description := kat.Description
	version := kat.Version
	if ns != "" {
		var filtered []CRDSummary
		for _, crd := range sortedCRDs {
			if crd.KatalogNamespace == ns {
				filtered = append(filtered, crd)
			}
		}
		sortedCRDs = filtered
		if nsSummary, ok := kat.Namespaces[ns]; ok {
			if nsSummary.Description != "" {
				description = nsSummary.Description
			}
			if nsSummary.Version != "" {
				version = nsSummary.Version
			}
		}
	}

	cc.renderTemplate(w, "katalog.html", KatalogData{
		CRDs:               sortedCRDs,
		OrkReady:           kat.OrkReady,
		DeletionProtection: kat.DeletionProtection,
		TotalCRDs:          len(sortedCRDs),
		TotalWorkers:       sumWorkers(sortedCRDs),
		TotalResources:     sumResources(sortedCRDs),
		HealthyCount:       countHealthyCRDs(sortedCRDs),
		KatalogName:        kat.Name,
		KatalogDescription: description,
		KatalogHealthy:     kat.Healthy,
		KatalogVersion:     version,
		KatalogAuthor:      kat.Author,
		KatalogLicense:     kat.License,
		DegradedReason:     kat.DegradedReason,
		StatusCounts:       computeStatusCounts(sortedCRDs),
		RuntimeVersion:     kat.RuntimeVersion,
		ClusterName:        kat.ClusterName,
	})
}

// buildDevAppSummary converts a ProjectInfoSummary into a DevAppSummary.
func buildDevAppSummary(proj ProjectInfoSummary) DevAppSummary {
	imageTag := proj.CurrentImage
	if idx := strings.LastIndex(imageTag, ":"); idx >= 0 {
		imageTag = imageTag[idx+1:]
	}
	port := proj.Port
	if port == "" {
		port = "8080"
	}
	svcURL := ""
	if proj.Name != "" && proj.Namespace != "" {
		svcURL = fmt.Sprintf("http://%s-svc.%s.svc.cluster.local:%s", proj.Name, proj.Namespace, port)
	}
	return DevAppSummary{
		Name:          proj.Name,
		Namespace:     proj.Namespace,
		Port:          port,
		Language:      proj.Language,
		CurrentImage:  proj.CurrentImage,
		ImageTag:      imageTag,
		ServiceURL:    svcURL,
		GitCommit:     proj.GitCommit,
		License:       proj.License,
		HasDockerfile: proj.HasDockerfile,
		HasFrontend:   proj.HasFrontend,
		HasSMTP:       proj.HasSMTP,
		HasSlack:      proj.HasSlack,
		HasCompose:    proj.HasCompose,
		SecretCount:   proj.SecretCount,
		ConfigCount:   proj.ConfigCount,
	}
}

// renderDevApps renders the developer-path view for a katalog created by ork doctor.
func (cc *ControlCenter) renderDevApps(w http.ResponseWriter, _ *http.Request, kat *KatalogResponse) {
	var apps []DevAppSummary
	for _, proj := range kat.Projects {
		s := buildDevAppSummary(proj)
		if s.Name != "" {
			apps = append(apps, s)
		}
	}
	// Sort apps by name for stable output.
	sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })

	cc.renderTemplate(w, "dev_apps.html", DevAppsData{
		KatalogName:    kat.Name,
		Apps:           apps,
		RuntimeVersion: kat.RuntimeVersion,
	})
}

// handleDevAppDetail renders /katalog/{katalog}/app/{appName} for developer katalogs.
func (cc *ControlCenter) handleDevAppDetail(w http.ResponseWriter, r *http.Request, katalogName, appName string) {
	inst, ok := cc.instanceByKatalogName(katalogName)
	if !ok {
		cc.handleNotFound(w, r)
		return
	}
	if inst.Katalog.CreatedBy != "orkdoctor" {
		cc.handleNotFound(w, r)
		return
	}
	proj, ok := inst.Katalog.Projects[appName]
	if !ok {
		cc.handleNotFound(w, r)
		return
	}
	cc.renderTemplate(w, "dev_app_detail.html", DevAppDetailData{
		KatalogName:    katalogName,
		App:            buildDevAppSummary(proj),
		RuntimeVersion: inst.Katalog.RuntimeVersion,
	})
}

func (cc *ControlCenter) handleCRDDetail(w http.ResponseWriter, r *http.Request, katalogName, crdName string) {
	inst, ok := cc.instanceByKatalogName(katalogName)
	if !ok {
		cc.renderError(w, r, fmt.Sprintf("Katalog %q not found", katalogName))
		return
	}

	endpoints := endpointInfoFor(inst.Katalog, crdName)
	summary := summaryFor(inst.Katalog, crdName)
	crd, err := inst.Client.FetchCRDDetail(crdName, endpoints, summary)
	if err != nil {
		log.Printf("WARN: fetch CRD %s: %v", crdName, err)
		// Render a degraded view rather than a hard error
		crd = &CRDDetail{
			Name:        crdName,
			State:       "offline",
			Healthy:     false,
			Description: "Unable to connect to Orkestra runtime",
			GVK:         "unknown",
			LastError:   err.Error(),
		}
	}

	// Merge gateway-owned webhook stats when the runtime advertises a gateway.
	if inst.GatewayEndpoint != "" && crd.State != "offline" {
		if gwStats, err := FetchGatewayCRDStats(inst.GatewayEndpoint, crdName); err != nil {
			log.Printf("WARN: gateway stats for %s: %v", crdName, err)
		} else if gwStats != nil {
			if gwStats.Admission != nil {
				crd.Admission = gwStats.Admission
			}
			if gwStats.Conversion != nil {
				crd.Conversion = gwStats.Conversion
			}
			if gwStats.DeletionProtection != nil {
				crd.DeletionProtection = gwStats.DeletionProtection
			}
			if gwStats.NamespaceProtection != nil {
				crd.NamespaceProtection = gwStats.NamespaceProtection
			}
		}
	}

	cc.renderTemplate(w, "crd.html", map[string]interface{}{
		"CRD":         crd,
		"KatalogName": katalogName,
	})
}

// handleCRList renders the CR instance list for one CRD.
// Route: /katalog/{katalog}/crd/{crd}/cr
func (cc *ControlCenter) handleCRList(w http.ResponseWriter, r *http.Request, instanceURL, katalogName, crdName string) {
	client := cc.clientFor(instanceURL)

	list, err := client.FetchCRList(instanceURL, crdName)
	if err != nil {
		cc.renderError(w, r, fmt.Sprintf("Could not load CR list for %s: \n%v", crdName, err))
		return
	}

	var idpEnabled bool
	var gatewayEndpoint string
	cc.mu.RLock()
	if inst, ok := cc.instances[instanceURL]; ok && inst.Katalog != nil {
		gatewayEndpoint = inst.GatewayEndpoint
		for _, crd := range inst.Katalog.CRDs {
			if strings.EqualFold(crd.Name, crdName) {
				idpEnabled = crd.IdpEnabled
				break
			}
		}
	}
	cc.mu.RUnlock()

	cc.renderTemplate(w, "cr_list.html", CRListView{
		KatalogName:     katalogName,
		Instance:        instanceURL,
		CRDName:         crdName,
		GVK:             list.GVK,
		Total:           list.Total,
		Items:           list.Items,
		BackURL:         fmt.Sprintf("/controlcenter/katalog/%s/crd/%s", katalogName, crdName),
		IdpEnabled:      idpEnabled,
		GatewayEndpoint: gatewayEndpoint,
	})
}

// handleIDPSchema proxies GET /api/v1/schema?target=<target> from the gateway.
// The gateway token is stored server-side — the browser never sees it.
func (cc *ControlCenter) handleIDPSchema(w http.ResponseWriter, r *http.Request, target string) {
	inst := cc.firstInstance()
	if inst == nil || inst.GatewayEndpoint == "" {
		http.Error(w, `{"error":"no gateway configured"}`, http.StatusServiceUnavailable)
		return
	}
	cc.proxyIDPRequest(w, r, inst.GatewayEndpoint+"/api/v1/schema?target="+url.QueryEscape(target), http.MethodGet, nil)
}

// handleIDPApply proxies POST /api/v1/apply to the gateway.
func (cc *ControlCenter) handleIDPApply(w http.ResponseWriter, r *http.Request) {
	inst := cc.firstInstance()
	if inst == nil || inst.GatewayEndpoint == "" {
		http.Error(w, `{"error":"no gateway configured"}`, http.StatusServiceUnavailable)
		return
	}
	cc.proxyIDPRequest(w, r, inst.GatewayEndpoint+"/api/v1/apply", http.MethodPost, r.Body)
}

// proxyIDPRequest forwards an IDP request to the gateway with the stored bearer token.
func (cc *ControlCenter) proxyIDPRequest(w http.ResponseWriter, _ *http.Request, target, method string, body io.Reader) {
	req, err := http.NewRequest(method, target, body) //nolint:noctx
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if cc.gatewayToken != "" {
		req.Header.Set("Authorization", "Bearer "+cc.gatewayToken)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, `{"error":"gateway unreachable"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	// If the gateway returned plain text (e.g. "Unauthorized"), wrap it so the
	// browser always receives valid JSON regardless of gateway error format.
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		fmt.Fprintf(w, `{"error":%q}`, strings.TrimSpace(string(respBody)))
		return
	}
	w.Write(respBody) //nolint:errcheck
}

// firstInstance returns the first instance that has a loaded Katalog, or nil.
func (cc *ControlCenter) firstInstance() *Instance {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	for _, inst := range cc.instances {
		if inst.Katalog != nil {
			return inst
		}
	}
	return nil
}

// instanceByURL returns the Instance for the given URL, or nil.
func (cc *ControlCenter) instanceByURL(instanceURL string) *Instance {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	inst := cc.instances[instanceURL]
	return inst
}

// handleIDPCreateForm renders the IDP create form (GET) or applies the CR (POST).
// Route: /katalog/{katalog}/crd/{crd}/cr/create
func (cc *ControlCenter) handleIDPCreateForm(w http.ResponseWriter, r *http.Request, instanceURL, katalogName, crdName string) {
	backURL := fmt.Sprintf("/controlcenter/katalog/%s/crd/%s/cr", katalogName, crdName)

	inst := cc.instanceByURL(instanceURL)
	if inst == nil || inst.Katalog == nil {
		http.Redirect(w, r, backURL, http.StatusSeeOther)
		return
	}

	var crdSummary *CRDSummary
	for i := range inst.Katalog.CRDs {
		if strings.EqualFold(inst.Katalog.CRDs[i].Name, crdName) {
			crdSummary = &inst.Katalog.CRDs[i]
			break
		}
	}
	if crdSummary == nil || !crdSummary.IdpEnabled || crdSummary.Target == "" {
		http.Redirect(w, r, backURL, http.StatusSeeOther)
		return
	}

	target := crdSummary.Target
	// Kind/APIVersion are display-only (page header) — the gateway builds
	// the CR from target, it doesn't need them from the form.
	kind, apiVersion := idpParseGVK(crdSummary.GVK)

	if r.Method == http.MethodPost {
		cc.handleIDPApplyForm(w, r, inst, target)
		return
	}

	sections, aliases, fetchErr := cc.fetchAllServeFields(inst, target)
	data := IDPFormData{
		KatalogName:      katalogName,
		CRDName:          crdName,
		Target:           target,
		Aliases:          aliases,
		Kind:             kind,
		APIVersion:       apiVersion,
		BackURL:          backURL,
		Namespaced:       crdSummary.Namespaced,
		RequireServeName: crdSummary.RequireServeName,
		Sections:         sections,
	}
	if fetchErr != nil {
		data.Error = "Could not load schema: " + fetchErr.Error()
	}
	cc.renderTemplate(w, "idp_form.html", data)
}

// FieldType defines the valid types for IDP additional fields.
// These are the only types that are allowed for idp.additionalFields fields.
type FieldType string

const (
	FieldTypeString  FieldType = "string"
	FieldTypeInteger FieldType = "integer"
	FieldTypeNumber  FieldType = "number"
	FieldTypeBoolean FieldType = "boolean"
	FieldTypeEnum    FieldType = "enum"
)

// serveFieldHint mirrors one entry of pkg/gateway/api.SchemaResponse.Fields
// (itself orktypes.ServeFieldConfig) — the gateway's flat, caller-facing field
// contract. It doesn't distinguish where a field routes to (spec, label,
// annotation) — the gateway resolves that from the Katalog, not the caller.
type serveFieldHint struct {
	Label       string            `json:"label"`
	Placeholder string            `json:"placeholder"`
	Hint        string            `json:"hint"`
	Order       int               `json:"order"`
	Category    string            `json:"category"`
	Required    bool              `json:"required"`
	Disabled    string            `json:"disabled"`
	When        []json.RawMessage `json:"when"`
	Or          []json.RawMessage `json:"or"`
	Type        string            `json:"type"`
	Enum        []string          `json:"enum"`
}

// buildServeField builds an ServeField for one gateway schema field entry.
// Type/enum come from the field's own declared type — the gateway's flat
// schema doesn't distinguish where a field routes to (spec, label,
// annotation), so neither does the form.
func buildServeField(name string, hint serveFieldHint) ServeField {
	label := hint.Label
	if label == "" {
		label = name
		if len(label) > 0 {
			label = strings.ToUpper(label[:1]) + label[1:]
		}
	}
	f := ServeField{
		Name:        name,
		Label:       label,
		Hint:        hint.Hint,
		Placeholder: hint.Placeholder,
		Required:    hint.Required,
		Category:    hint.Category,
		Disabled:    hint.Disabled,
	}
	// Encode when/or as JSON strings for the template to embed as data attributes.
	if len(hint.When) > 0 {
		if b, err := json.Marshal(hint.When); err == nil {
			f.WhenJSON = string(b)
		}
	}
	if len(hint.Or) > 0 {
		if b, err := json.Marshal(hint.Or); err == nil {
			f.OrJSON = string(b)
		}
	}
	switch {
	case hint.Type == string(FieldTypeEnum) && len(hint.Enum) > 0:
		f.InputType = "select"
		f.Enum = hint.Enum
	case hint.Type == string(FieldTypeBoolean):
		f.InputType = "checkbox"
	case hint.Type == string(FieldTypeInteger) || hint.Type == string(FieldTypeNumber):
		f.InputType = "number"
	default:
		f.InputType = "text"
	}
	return f
}

// fetchAllServeFields fetches the flat field schema for target from the gateway.
// It also returns the alias names declared for this CRD so the form can render
// a surface dropdown when more than one entry point exists.
func (cc *ControlCenter) fetchAllServeFields(inst *Instance, target string) ([]IDPSection, []string, error) {
	if inst.GatewayEndpoint == "" {
		return nil, nil, fmt.Errorf("no gateway endpoint configured")
	}
	if cc.gatewayToken == "" {
		return nil, nil, fmt.Errorf("GATEWAY_TOKEN not set")
	}

	req, _ := http.NewRequest(http.MethodGet, inst.GatewayEndpoint+"/api/v1/schema?target="+url.QueryEscape(target), nil) //nolint:noctx
	req.Header.Set("Authorization", "Bearer "+cc.gatewayToken)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("%s", strings.TrimSpace(string(body)))
	}

	var schemaResp struct {
		Aliases []string                  `json:"aliases"`
		Fields  map[string]serveFieldHint `json:"fields"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&schemaResp); err != nil {
		return nil, nil, err
	}

	type orderedField struct {
		field ServeField
		order int
	}
	var ordered []orderedField
	for name, hint := range schemaResp.Fields {
		ordered = append(ordered, orderedField{field: buildServeField(name, hint), order: hint.Order})
	}

	sort.Slice(ordered, func(i, j int) bool {
		oi, oj := ordered[i].order, ordered[j].order
		if oi == 0 && oj != 0 {
			return false
		}
		if oi != 0 && oj == 0 {
			return true
		}
		if oi != oj {
			return oi < oj
		}
		return ordered[i].field.Name < ordered[j].field.Name
	})
	var sections []IDPSection
	for _, o := range ordered {
		f := o.field
		title := f.Category
		if title == "" {
			title = "Fields"
		}
		if len(sections) == 0 || sections[len(sections)-1].Title != title {
			sections = append(sections, IDPSection{Title: title})
		}
		sections[len(sections)-1].Fields = append(sections[len(sections)-1].Fields, f)
	}
	return sections, schemaResp.Aliases, nil
}

// handleIDPApplyForm processes the IDP form POST: forwards the flat
// target-mode payload the form submitted to the Gateway API. The
// gateway builds the CR from target — this handler doesn't construct one.
func (cc *ControlCenter) handleIDPApplyForm(w http.ResponseWriter, r *http.Request, inst *Instance, target string) {
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":"invalid request body"}`)
		return
	}
	// If the form submitted a surface (alias or primary), use it. The gateway
	// validates the surface and rejects unknown targets, so we don't need to
	// whitelist here. Fall back to the route-resolved primary target when the
	// form omits it (shouldn't happen, but safe default).
	if submitted, ok := body["target"].(string); !ok || submitted == "" {
		body["target"] = target
	}

	payload, _ := json.Marshal(body)
	applyURL := inst.GatewayEndpoint + "/api/v1/apply"
	if r.URL.Query().Get("dryRun") == "true" {
		applyURL += "?dryRun=true"
	}
	cc.proxyIDPRequest(w, r, applyURL, http.MethodPost, bytes.NewReader(payload))
}

// idpParseGVK splits "group/version, Kind=Kind" into (kind, apiVersion).
func idpParseGVK(gvk string) (kind, apiVersion string) {
	parts := strings.SplitN(gvk, ",", 2)
	if len(parts) == 2 {
		apiVersion = strings.TrimSpace(parts[0])
		kind = strings.TrimPrefix(strings.TrimSpace(parts[1]), "Kind=")
	}
	return
}

// handleCRDetail renders one CR instance with its children and events.
// Route: /katalog/{katalog}/crd/{crd}/cr/{ns}/{name}  (namespace may be empty)
func (cc *ControlCenter) handleCRDetail(w http.ResponseWriter, r *http.Request, instanceURL, katalogName, crdName, namespace, name string) {
	client := cc.clientFor(instanceURL)

	detail, err := client.FetchCRDetail(instanceURL, crdName, namespace, name)
	if err != nil {
		if namespace != "" {
			cc.renderError(w, r, fmt.Sprintf("Could not load CR '%s/%s': \n%v", namespace, name, err))
		} else {
			cc.renderError(w, r, fmt.Sprintf("Could not load CR '%s': \n%v", name, err))
		}
		return
	}

	// Events are best-effort — never block or error on failure
	var events []CREvent
	var eventTotal int
	if evResp, err := client.FetchCREvents(instanceURL, crdName, namespace, name); err == nil {
		events = evResp.Events
		eventTotal = evResp.Total
	}

	// Extract phase from status for template convenience
	phase := ""
	if detail.Status != nil {
		if p, ok := detail.Status["phase"].(string); ok {
			phase = p
		}
	}

	cc.renderTemplate(w, "cr_detail.html", CRDetailView{
		KatalogName: katalogName,
		Instance:    instanceURL,
		CRDName:     crdName,
		CR:          *detail,
		Events:      events,
		EventTotal:  eventTotal,
		Phase:       phase,
		ChildGroups: normalizeChildGroups(detail.Children),
		BackURL:     fmt.Sprintf("/controlcenter/katalog/%s/crd/%s", katalogName, crdName),
	})
}

func (cc *ControlCenter) handleListInstances(w http.ResponseWriter, r *http.Request) {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	type InstanceDTO struct {
		URL       string    `json:"url"`
		Healthy   bool      `json:"healthy"`
		LastError string    `json:"lastError,omitempty"`
		LastCheck time.Time `json:"lastCheck"`
	}

	out := make([]InstanceDTO, 0, len(cc.instances))
	for _, inst := range cc.instances {
		out = append(out, InstanceDTO{
			URL:       inst.URL,
			Healthy:   inst.Healthy,
			LastError: inst.LastError,
			LastCheck: inst.LastCheck,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"instances": out,
	})
}

func (cc *ControlCenter) handleAddInstance(w http.ResponseWriter, r *http.Request) {
	var req struct{ URL string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if err := cc.AddInstance(req.URL); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "added"})
}

func (cc *ControlCenter) handleUpdateInstance(w http.ResponseWriter, r *http.Request) {
	// Path: /api/instances/encodedOldURL
	encoded := strings.TrimPrefix(r.URL.Path, "/api/instances/")
	oldURL, err := url.PathUnescape(encoded)
	if err != nil {
		http.Error(w, `{"error":"invalid URL"}`, http.StatusBadRequest)
		return
	}
	var req struct{ URL string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	newURL := normalizeURL(req.URL)
	if err := cc.UpdateInstance(oldURL, newURL); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func (cc *ControlCenter) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	encoded := strings.TrimPrefix(r.URL.Path, "/api/instances/")
	urlStr, err := url.PathUnescape(encoded)
	if err != nil {
		http.Error(w, `{"error":"invalid URL"}`, http.StatusBadRequest)
		return
	}
	if err := cc.DeleteInstance(urlStr); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// normalizeURL adds scheme and removes trailing slash.
func normalizeURL(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return ""
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "http://" + u
	}
	return strings.TrimSuffix(u, "/")
}

// ─────────────────────────────────────────────────────────────────────────────
// Asset and utility handlers
// ─────────────────────────────────────────────────────────────────────────────

func (cc *ControlCenter) serveAsset(w http.ResponseWriter, r *http.Request, filePath string) {
	data, err := Assets.ReadFile("assets/" + filePath)
	if err != nil {
		cc.handleNotFound(w, r)
		return
	}
	switch {
	case strings.HasSuffix(filePath, ".css"):
		w.Header().Set("Content-Type", "text/css")
	case strings.HasSuffix(filePath, ".js"):
		w.Header().Set("Content-Type", "application/javascript")
	case strings.HasSuffix(filePath, ".png"):
		w.Header().Set("Content-Type", "image/png")
	case strings.HasSuffix(filePath, ".svg"):
		w.Header().Set("Content-Type", "image/svg+xml")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Write(data)
}

func (cc *ControlCenter) handleMetricsPage(w http.ResponseWriter, _ *http.Request) {
	cc.renderTemplate(w, "metrics.html", nil)
}

func (cc *ControlCenter) handleDebugFile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		filePath = "assets/static/logo.png"
	}
	fmt.Fprintf(w, "Reading: %s\n\n", filePath)
	data, err := Assets.ReadFile(filePath)
	if err != nil {
		fmt.Fprintf(w, "ERROR: %v\n", err)
		return
	}
	fmt.Fprintf(w, "OK: %d bytes\n", len(data))

	entries, _ := Assets.ReadDir("assets/static")
	fmt.Fprintf(w, "\nassets/static/:\n")
	for _, e := range entries {
		fmt.Fprintf(w, "  %s\n", e.Name())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Aggregate helpers
// ─────────────────────────────────────────────────────────────────────────────

func sumWorkers(crds []CRDSummary) int {
	n := 0
	for _, c := range crds {
		n += c.Workers
	}
	return n
}

func sumResources(crds []CRDSummary) int {
	n := 0
	for _, c := range crds {
		n += c.ResourceCount
	}
	return n
}

func countHealthyCRDs(crds []CRDSummary) int {
	n := 0
	for _, c := range crds {
		if c.Healthy {
			n++
		}
	}
	return n
}

func computeStatusCounts(crds []CRDSummary) StatusCounts {
	var sc StatusCounts
	for _, c := range crds {
		switch c.State {
		case "healthy":
			sc.Healthy++
		case "degraded":
			sc.Degraded++
		case "started":
			sc.Started++
		case "gated":
			sc.Gated++
		default:
			sc.Pending++
		}
	}
	return sc
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
