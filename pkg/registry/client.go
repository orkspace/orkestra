// pkg/registry/client.go
//
// ORAS-based OCI client for pushing and pulling Orkestra artifacts.
//
// Authentication uses ~/.docker/config.json via oras.land/oras-go/v2.
// No separate login step — docker login ghcr.io is sufficient.
//
// Push: validates the directory, reads each file, pushes as OCI layers.
// Pull: fetches the manifest, extracts layers to the cache directory.
// Info: fetches the manifest only, reads annotations.
// List: fetches the index pattern from the registry root.
//
// Authoring-time only: every caller (ork push/pull/inspect/patterns and the
// e2e/validate/simulate commands) is already !runtime && !gateway tagged.
// The runtime and gateway only ever read an already-merged katalog.yaml key
// from a ConfigMap — motif/registry/helm imports are expanded before that
// point, not by either binary. No stub needed here: nothing in the
// runtime/gateway build graph calls into this file.

//go:build !runtime && !gateway

package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	godigest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/content/memory"

	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// Client wraps ORAS for Orkestra pattern operations.
type Client struct {
	credStore credentials.Store
}

// NewClient returns a Client with credentials loaded from the Docker config.
func NewClient() (*Client, error) {
	store, err := credentials.NewStoreFromDocker(credentials.StoreOptions{
		AllowPlaintextPut: false,
	})
	if err != nil {
		return nil, fmt.Errorf("loading docker credentials: %w", err)
	}
	return &Client{credStore: store}, nil
}

// Push validates the directory, auto-detects the pattern kind, and pushes all
// files to the registry. Returns the manifest digest on success.
// opts carries optional gate metadata embedded as OCI annotations on the published artifact.
func (c *Client) Push(ctx context.Context, ref *Ref, dir string, opts PushOptions, progress func(file string, size int64)) (string, error) {
	patternKind, spec, files, err := ValidatePatternDirectory(dir)
	if err != nil {
		return "", fmt.Errorf("validation failed: %w", err)
	}

	meta, err := LoadPatternMeta(dir, spec)
	if err != nil {
		return "", fmt.Errorf("reading metadata: %w", err)
	}

	if opts.E2E != nil {
		meta.E2E = opts.E2E
	}
	if opts.Simulate != nil {
		meta.Simulate = opts.Simulate
	}
	if opts.Intent != nil {
		meta.Intent = opts.Intent
	}
	if opts.Typed != nil {
		meta.Typed = opts.Typed
	}
	if opts.RuntimeVersion != "" {
		meta.RuntimeVersion = opts.RuntimeVersion
	}

	store := memory.New()

	var descs []ocispec.Descriptor
	for _, f := range files {
		path := filepath.Join(dir, f)
		data, err := readLocal(path)
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", f, err)
		}
		if progress != nil {
			progress(f, int64(len(data)))
		}
		desc := content.NewDescriptorFromBytes(mediaTypeForPatternFile(f, patternKind), data)
		desc.Annotations = map[string]string{
			"org.opencontainers.image.title": f,
		}
		if err := store.Push(ctx, desc, bytes.NewReader(data)); err != nil {
			return "", fmt.Errorf("staging %s: %w", f, err)
		}
		descs = append(descs, desc)
	}

	annotations := artifactMetaToAnnotations(meta, ref)
	manifestDesc, err := oras.Pack(ctx, store, spec.MediaType, descs, oras.PackOptions{
		PackImageManifest:   true,
		ManifestAnnotations: annotations,
	})
	if err != nil {
		return "", fmt.Errorf("packing manifest: %w", err)
	}

	if err := store.Tag(ctx, manifestDesc, ref.Tag); err != nil {
		return "", fmt.Errorf("tagging manifest: %w", err)
	}

	repo, err := c.remoteRepo(ref)
	if err != nil {
		return "", err
	}

	if _, err := oras.Copy(ctx, store, ref.Tag, repo, ref.Tag, oras.DefaultCopyOptions); err != nil {
		return "", fmt.Errorf("pushing: %w", err)
	}

	entry := PatternEntry{
		Name:          meta.Name,
		LatestVersion: meta.Version,
		Description:   meta.Description,
		Tags:          meta.Tags,
		Author:        meta.Author,
		Kind:          string(patternKind),
	}
	if meta.E2E != nil {
		entry.E2EStatus = meta.E2E.Status
	}
	if meta.Simulate != nil {
		entry.SimulateStatus = meta.Simulate.Status
	}
	if meta.Deprecated != nil {
		entry.Deprecated = true
	}
	if err := c.updateIndex(ctx, ref, entry); err != nil {
		fmt.Fprintf(os.Stderr, "warning: index update failed: %v\n", err)
	}

	return manifestDesc.Digest.String(), nil
}

// Pull fetches an pattern from the registry into the local cache.
// Returns the cache directory path.
func (c *Client) Pull(ctx context.Context, ref *Ref, refresh bool) (string, error) {
	cacheDir, err := ref.CachePath()
	if err != nil {
		return "", err
	}

	if !refresh && ref.IsCached() {
		return cacheDir, nil
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("creating cache dir: %w", err)
	}

	store, err := file.New(cacheDir)
	if err != nil {
		return "", fmt.Errorf("creating file store: %w", err)
	}
	defer store.Close()

	repo, err := c.remoteRepo(ref)
	if err != nil {
		return "", err
	}

	if _, err := oras.Copy(ctx, repo, ref.Tag, store, ref.Tag, oras.DefaultCopyOptions); err != nil {
		os.RemoveAll(cacheDir)
		return "", fmt.Errorf("pulling: %w", err)
	}

	return cacheDir, nil
}

// Info fetches manifest metadata without downloading the pattern files.
func (c *Client) Info(ctx context.Context, ref *Ref) (*PatternInfo, error) {
	repo, err := c.remoteRepo(ref)
	if err != nil {
		return nil, err
	}

	desc, _, err := repo.FetchReference(ctx, ref.Tag)
	if err != nil {
		return nil, fmt.Errorf("fetching manifest: %w", err)
	}

	rc, err := repo.Fetch(ctx, desc)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}
	defer rc.Close()

	var manifest struct {
		Annotations map[string]string `json:"annotations"`
		Layers      []struct {
			Digest      string            `json:"digest"`
			Size        int64             `json:"size"`
			Annotations map[string]string `json:"annotations"`
		} `json:"layers"`
	}
	if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decoding manifest: %w", err)
	}

	meta := annotationsToMeta(manifest.Annotations)

	info := &PatternInfo{
		Ref:    ref,
		Digest: desc.Digest.String(),
		Size:   desc.Size,
		Meta:   meta,
	}

	for _, layer := range manifest.Layers {
		if name := layer.Annotations["org.opencontainers.image.title"]; name != "" {
			info.Files = append(info.Files, FileEntry{Name: name, Size: layer.Size, Digest: layer.Digest})
		}
	}

	if ts, ok := manifest.Annotations["org.opencontainers.image.created"]; ok {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			info.PushedAt = t
		}
	}

	return info, nil
}

// ViewFile fetches the raw content of a single OCI layer blob.
// Use a FileEntry from PatternInfo.Files — both Digest and Size are required
// so the registry can validate the Content-Length on the response.
func (c *Client) ViewFile(ctx context.Context, ref *Ref, f FileEntry) ([]byte, error) {
	repo, err := c.remoteRepo(ref)
	if err != nil {
		return nil, err
	}
	desc := ocispec.Descriptor{
		Digest: godigest.Digest(f.Digest),
		Size:   f.Size,
	}
	rc, err := repo.Blobs().Fetch(ctx, desc)
	if err != nil {
		return nil, fmt.Errorf("fetching blob: %w", err)
	}
	defer rc.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(rc); err != nil {
		return nil, fmt.Errorf("reading blob: %w", err)
	}
	return buf.Bytes(), nil
}

// List fetches the pattern index from the given registry URL.
// Returns an empty index (not an error) when no artifacts have been pushed yet.
func (c *Client) List(ctx context.Context, registryURL string) (*PatternIndex, error) {
	if registryURL == "" {
		registryURL = DefaultPatternRegistry
	}
	clean := strings.TrimSuffix(strings.TrimPrefix(registryURL, "oci://"), "/")
	idxRef, err := parseRef(clean + "/index:latest")
	if err != nil {
		return nil, fmt.Errorf("building index ref: %w", err)
	}
	index, err := c.fetchIndex(ctx, idxRef)
	if err != nil {
		return nil, err
	}
	return index, nil
}

// ── Index management ──────────────────────────────────────────────────────────

func indexRefFrom(ref *Ref) (*Ref, error) {
	lastSlash := strings.LastIndex(ref.Repository, "/")
	if lastSlash < 0 {
		return nil, fmt.Errorf("cannot derive index path from ref %q", ref.Full)
	}
	namespace := ref.Repository[:lastSlash]
	return parseRef(ref.Registry + "/" + namespace + "/index:latest")
}

func (c *Client) fetchIndex(ctx context.Context, idxRef *Ref) (*PatternIndex, error) {
	repo, err := c.remoteRepo(idxRef)
	if err != nil {
		return nil, err
	}

	tmp, err := os.MkdirTemp("", "ork-index-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	store, err := file.New(tmp)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	if _, err := oras.Copy(ctx, repo, idxRef.Tag, store, idxRef.Tag, oras.DefaultCopyOptions); err != nil {
		return nil, err
	}

	data, err := readLocal(filepath.Join(tmp, "index.json"))
	if err != nil {
		return nil, fmt.Errorf("reading index blob: %w", err)
	}

	var index PatternIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("decoding index: %w", err)
	}
	return &index, nil
}

func (c *Client) pushIndex(ctx context.Context, idxRef *Ref, index *PatternIndex) error {
	data, err := json.Marshal(index)
	if err != nil {
		return err
	}

	store := memory.New()

	blob := content.NewDescriptorFromBytes("application/json", data)
	if err := store.Push(ctx, blob, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("staging index blob: %w", err)
	}

	blob.Annotations = map[string]string{
		"org.opencontainers.image.title": "index.json",
	}

	manifestDesc, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, indexMediaType, oras.PackManifestOptions{
		Layers: []ocispec.Descriptor{blob},
	})
	if err != nil {
		return fmt.Errorf("packing index manifest: %w", err)
	}

	if err := store.Tag(ctx, manifestDesc, idxRef.Tag); err != nil {
		return fmt.Errorf("tagging index: %w", err)
	}

	repo, err := c.remoteRepo(idxRef)
	if err != nil {
		return err
	}

	if _, err := oras.Copy(ctx, store, idxRef.Tag, repo, idxRef.Tag, oras.DefaultCopyOptions); err != nil {
		return fmt.Errorf("pushing index: %w", err)
	}
	return nil
}

func (c *Client) updateIndex(ctx context.Context, ref *Ref, entry PatternEntry) error {
	idxRef, err := indexRefFrom(ref)
	if err != nil {
		return err
	}

	index, err := c.fetchIndex(ctx, idxRef)
	if err != nil {
		index = &PatternIndex{}
	}

	found := false
	for i, e := range index.Entries {
		if e.Name == entry.Name {
			index.Entries[i] = entry
			found = true
			break
		}
	}
	if !found {
		index.Entries = append(index.Entries, entry)
	}
	index.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	return c.pushIndex(ctx, idxRef, index)
}

// ListVersions lists up to maxN most recent versions of a pattern by listing
// OCI tags and fetching metadata for each. Results are sorted newest-first by
// semantic version. Tags that cannot be fetched are silently skipped.
func (c *Client) ListVersions(ctx context.Context, ref *Ref, maxN int) ([]*VersionInfo, error) {
	repo, err := c.remoteRepo(ref)
	if err != nil {
		return nil, err
	}

	var tags []string
	if err := repo.Tags(ctx, "", func(batch []string) error {
		tags = append(tags, batch...)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("listing tags: %w", err)
	}

	// Deduplicate by digest: floating tags (latest, stable) that point to the
	// same manifest as a versioned tag don't appear twice. Versioned tags win.
	seen := map[string]string{} // digest → winning tag
	infoByDigest := map[string]*PatternInfo{}

	for _, tag := range tags {
		tagRef := &Ref{
			Registry:   ref.Registry,
			Repository: ref.Repository,
			Tag:        tag,
			Full:       ref.Registry + "/" + ref.Repository + ":" + tag,
		}
		info, err := c.Info(ctx, tagRef)
		if err != nil {
			continue
		}
		existing, collision := seen[info.Digest]
		if collision {
			// Replace the existing tag only if the new one looks like a real version.
			if looksLikeVersion(tag) && !looksLikeVersion(existing) {
				seen[info.Digest] = tag
				info.Meta.Version = tag
				infoByDigest[info.Digest] = info
			}
			continue
		}
		seen[info.Digest] = tag
		infoByDigest[info.Digest] = info
	}

	versions := make([]*VersionInfo, 0, len(infoByDigest))
	for digest, info := range infoByDigest {
		versions = append(versions, &VersionInfo{
			Tag:      seen[digest],
			PushedAt: info.PushedAt,
			Meta:     info.Meta,
			Digest:   digest,
		})
	}
	// Mirrors the index table: most recently pushed = latest.
	slices.SortFunc(versions, func(a, b *VersionInfo) int {
		return b.PushedAt.Compare(a.PushedAt) // descending
	})

	if maxN > 0 && len(versions) > maxN {
		versions = versions[:maxN]
	}

	return versions, nil
}

// looksLikeVersion returns true when a tag appears to be a pinned version
// (starts with 'v' or a digit) rather than a floating label like "latest".
func looksLikeVersion(tag string) bool {
	if len(tag) == 0 {
		return false
	}
	return tag[0] == 'v' || (tag[0] >= '0' && tag[0] <= '9')
}

// remoteRepo returns an authenticated ORAS remote.Repository for the ref.
func (c *Client) remoteRepo(ref *Ref) (*remote.Repository, error) {
	repo, err := remote.NewRepository(ref.Full)
	if err != nil {
		return nil, fmt.Errorf("invalid reference %q: %w", ref.Full, err)
	}
	repo.Client = &auth.Client{
		Client:     retry.DefaultClient,
		Cache:      auth.DefaultCache,
		Credential: credentials.Credential(c.credStore),
	}
	return repo, nil
}

// artifactMetaToAnnotations converts PatternMeta to OCI manifest annotations.
func artifactMetaToAnnotations(meta *PatternMeta, ref *Ref) map[string]string {
	ann := map[string]string{
		"org.opencontainers.image.created":     time.Now().UTC().Format(time.RFC3339),
		"org.opencontainers.image.title":       meta.Name,
		"org.opencontainers.image.version":     meta.Version,
		"org.opencontainers.image.description": meta.Description,
		"io.orkestra.pattern.kind":             string(meta.Kind),
		"io.orkestra.pattern.name":             meta.Name,
		"io.orkestra.pattern.version":          meta.Version,
		"io.orkestra.pattern.author":           meta.Author,
		"io.orkestra.pattern.license":          meta.License,
		"io.orkestra.pattern.tags":             strings.Join(meta.Tags, ","),
	}
	if meta.Author != "" {
		ann["org.opencontainers.image.authors"] = meta.Author
	}
	if meta.E2E != nil {
		ann["io.orkestra.e2e.status"] = meta.E2E.Status
		if meta.E2E.Duration != "" {
			ann["io.orkestra.e2e.duration"] = meta.E2E.Duration
		}
		if meta.E2E.TestedAt != "" {
			ann["io.orkestra.e2e.tested_at"] = meta.E2E.TestedAt
		}
		if meta.E2E.Runner != "" {
			ann["io.orkestra.e2e.runner"] = meta.E2E.Runner
		}
		if meta.E2E.Assertions > 0 {
			ann["io.orkestra.e2e.assertions"] = strconv.Itoa(meta.E2E.Assertions)
		}
	}
	if meta.Simulate != nil {
		ann["io.orkestra.simulate.status"] = meta.Simulate.Status
		if meta.Simulate.Duration != "" {
			ann["io.orkestra.simulate.duration"] = meta.Simulate.Duration
		}
		if meta.Simulate.TestedAt != "" {
			ann["io.orkestra.simulate.tested_at"] = meta.Simulate.TestedAt
		}
		if meta.Simulate.Assertions > 0 {
			ann["io.orkestra.simulate.assertions"] = strconv.Itoa(meta.Simulate.Assertions)
		}
	}
	if meta.Intent != nil {
		ann["io.orkestra.intent.status"] = meta.Intent.Status
		if meta.Intent.Target != "" {
			ann["io.orkestra.intent.target"] = meta.Intent.Target
		}
		if meta.Intent.TestedAt != "" {
			ann["io.orkestra.intent.tested_at"] = meta.Intent.TestedAt
		}
	}
	if meta.Typed != nil {
		if meta.Typed.HasHooks {
			ann["io.orkestra.katalog.has_hooks"] = "true"
		}
		if meta.Typed.HasConstructor {
			ann["io.orkestra.katalog.has_constructor"] = "true"
		}
		if meta.Typed.HasHooks || meta.Typed.HasConstructor {
			ann["io.orkestra.katalog.typed"] = "true"
		}
	}
	if meta.Deprecated != nil {
		ann["io.orkestra.katalog.deprecated"] = "true"
		if meta.Deprecated.MigratedTo != "" {
			ann["io.orkestra.katalog.deprecated.migrated_to"] = meta.Deprecated.MigratedTo
		}
		if meta.Deprecated.Message != "" {
			ann["io.orkestra.katalog.deprecated.message"] = meta.Deprecated.Message
		}
		if meta.Deprecated.TimelineFrom != "" {
			ann["io.orkestra.katalog.deprecated.timeline_from"] = meta.Deprecated.TimelineFrom
		}
		if meta.Deprecated.TimelineTo != "" {
			ann["io.orkestra.katalog.deprecated.timeline_to"] = meta.Deprecated.TimelineTo
		}
	}
	if meta.RuntimeVersion != "" {
		ann["io.orkestra.katalog.runtime_version"] = meta.RuntimeVersion
	}
	return ann
}

// annotationsToMeta reconstructs PatternMeta from OCI manifest annotations.
func annotationsToMeta(ann map[string]string) *PatternMeta {
	tags := []string{}
	name := ann["io.orkestra.pattern.name"]
	if name == "" {
		name = ann["io.orkestra.pattern.name"]
	}
	version := ann["io.orkestra.pattern.version"]
	if version == "" {
		version = ann["io.orkestra.pattern.version"]
	}
	author := ann["io.orkestra.pattern.author"]
	if author == "" {
		author = ann["io.orkestra.pattern.author"]
	}
	license := ann["io.orkestra.pattern.license"]
	if license == "" {
		license = ann["io.orkestra.pattern.license"]
	}
	kindStr := ann["io.orkestra.pattern.kind"]
	if t := ann["io.orkestra.pattern.tags"]; t != "" {
		tags = strings.Split(t, ",")
	} else if t := ann["io.orkestra.pattern.tags"]; t != "" {
		tags = strings.Split(t, ",")
	}
	meta := &PatternMeta{
		Kind:        PatternKind(kindStr),
		Name:        name,
		Version:     version,
		Description: ann["org.opencontainers.image.description"],
		Author:      author,
		License:     license,
		Tags:        tags,
	}
	if status := ann["io.orkestra.e2e.status"]; status != "" {
		n, _ := strconv.Atoi(ann["io.orkestra.e2e.assertions"])
		meta.E2E = &PatternE2E{
			Status:     status,
			Duration:   ann["io.orkestra.e2e.duration"],
			TestedAt:   ann["io.orkestra.e2e.tested_at"],
			Runner:     ann["io.orkestra.e2e.runner"],
			Assertions: n,
		}
	}
	if status := ann["io.orkestra.simulate.status"]; status != "" {
		n, _ := strconv.Atoi(ann["io.orkestra.simulate.assertions"])
		meta.Simulate = &PatternSimulate{
			Status:     status,
			Duration:   ann["io.orkestra.simulate.duration"],
			TestedAt:   ann["io.orkestra.simulate.tested_at"],
			Assertions: n,
		}
	}
	if status := ann["io.orkestra.intent.status"]; status != "" {
		meta.Intent = &PatternIntent{
			Status:   status,
			Target:   ann["io.orkestra.intent.target"],
			TestedAt: ann["io.orkestra.intent.tested_at"],
		}
	}
	if ann["io.orkestra.katalog.typed"] == "true" {
		meta.Typed = &PatternTyped{
			HasHooks:       ann["io.orkestra.katalog.has_hooks"] == "true",
			HasConstructor: ann["io.orkestra.katalog.has_constructor"] == "true",
		}
	}
	if ann["io.orkestra.katalog.deprecated"] == "true" {
		meta.Deprecated = &PatternDeprecated{
			MigratedTo:   ann["io.orkestra.katalog.deprecated.migrated_to"],
			Message:      ann["io.orkestra.katalog.deprecated.message"],
			TimelineFrom: ann["io.orkestra.katalog.deprecated.timeline_from"],
			TimelineTo:   ann["io.orkestra.katalog.deprecated.timeline_to"],
		}
	}
	meta.RuntimeVersion = ann["io.orkestra.katalog.runtime_version"]
	return meta
}
