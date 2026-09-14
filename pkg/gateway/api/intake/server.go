package intake

import (
	"context"
	"fmt"
	"sync"

	"github.com/orkspace/orkestra/pkg/gateway/api"
	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// Server holds the resolved webhook Set and wires each entry's route.
// Mirrors api.APIServer's shape — same self-bootstrap/rotation and reload
// pattern — kept as a separate type rather than a method on APIServer
// itself: this package already imports pkg/gateway/api (for
// ApplyTargetFields/ResolveSecretRef), so the reverse import isn't
// possible. The caller composes the two servers instead — see
// cmd/internal/gateway.go.
type Server struct {
	mu       sync.RWMutex
	set      *Set
	kube     kubeclient.Interface
	clusters *api.ClusterRegistry
	kat      *katalog.Katalog
	ownNS    string
}

// NewIntakeServer resolves every enabled webhook entry's credentials and
// returns a ready-to-register server. Returns (nil, nil) when
// gateway.webhooks declares no sources — the caller skips registration.
func NewIntakeServer(ctx context.Context, kat *katalog.Katalog, kube kubeclient.Interface, clusters *api.ClusterRegistry, ownNS string) (*Server, error) {
	if kat == nil || kat.Gateway == nil || kat.Gateway.Webhooks.Empty() {
		return nil, nil
	}

	set, err := Load(ctx, kat.Gateway.Webhooks, kube, ownNS)
	if err != nil {
		return nil, fmt.Errorf("loading gateway webhooks: %w", err)
	}

	return &Server{set: set, kube: kube, clusters: clusters, kat: kat, ownNS: ownNS}, nil
}

// Reload re-resolves every entry's credentials (recreating any missing
// secrets) and atomically replaces the in-memory Set. Called from the same
// reload cycle as api.APIServer.ReloadTokens — see cmd/internal/gateway.go.
func (s *Server) Reload(ctx context.Context) error {
	set, err := Load(ctx, s.kat.Gateway.Webhooks, s.kube, s.ownNS)
	if err != nil {
		return fmt.Errorf("reload gateway webhooks: %w", err)
	}
	s.mu.Lock()
	s.set = set
	s.mu.Unlock()
	return nil
}

// current reads the current Set through the reload mutex.
func (s *Server) current() *Set {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.set
}

// Register wires every resolved entry's route onto reg. Unlike
// api.APIServer's routes, these are NOT wrapped in Bearer-token auth
// middleware — each handler verifies the request itself (HMAC signature,
// static token, or Slack's signing scheme), the same way the source's own
// delivery mechanism authenticates on every other platform that receives
// its webhooks.
func (s *Server) Register(reg api.Registrar, notes orktypes.NoteRegistry) {
	set := s.current()

	for _, src := range set.GitHub {
		reg.Register(src.Config.Path, NewGitHubHandler(src, s.kube, s.clusters, s.kat, notes))
	}
	for _, src := range set.GitLab {
		reg.Register(src.Config.Path, NewGitLabHandler(src, s.kube, s.clusters, s.kat, notes))
	}
	for _, src := range set.Slack {
		reg.Register(src.Config.Path, NewSlackHandler(src, s.kube, s.clusters, s.kat, notes, NewHTTPSlackClient()))
	}
	for _, src := range set.Generic {
		reg.Register(src.Config.Path, NewGenericHandler(src, s.kube, s.clusters, s.kat, notes))
	}

	total := len(set.GitHub) + len(set.GitLab) + len(set.Slack) + len(set.Generic)
	if total > 0 {
		logger.Info().
			Int("github", len(set.GitHub)).
			Int("gitlab", len(set.GitLab)).
			Int("slack", len(set.Slack)).
			Int("generic", len(set.Generic)).
			Msg("gateway webhook intake routes registered")
	}
}
