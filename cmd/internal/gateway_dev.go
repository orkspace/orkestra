// cmd/internal/gateway_dev.go
//
// Local gateway — HTTP-only variant for development. Handles the Serve layer
// (Gateway API + intake webhooks) without TLS or a live cluster. Admission and
// conversion webhooks are skipped; a warning is printed at startup.
//
// Included in dev builds (make ork). Excluded from production gateway builds.

//go:build !runtime && !gateway

package internal

import (
	"context"

	"github.com/orkspace/orkestra/domain"
	apigateway "github.com/orkspace/orkestra/pkg/gateway/api"
	"github.com/orkspace/orkestra/pkg/gateway/api/intake"
	"github.com/orkspace/orkestra/pkg/health"
	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/katalog/pipeline"
	"github.com/orkspace/orkestra/pkg/konfig"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/merger"
	ork "github.com/orkspace/orkestra/pkg/orkestra"
	"github.com/orkspace/orkestra/pkg/utils"
)

// KonductGatewayDev starts a local HTTP-only gateway.
//
// TLS, WebhookServer, and /katalog routes that depend on webhook state are all
// omitted. The Gateway API (POST /api/v1/apply, GET /api/v1/resources/, etc.)
// and intake webhooks run on the plain HTTP health port — identical to the
// in-cluster API surface, with no certificate setup required.
func KonductGatewayDev(kfg *konfig.Konfig, m *merger.Merger, ctx context.Context) {

	// ── 1. Instance + Katalog ─────────────────────────────────────────────────
	kfg.SetInstance(konfig.Gateway())

	kat := pipeline.NewKatalog(kfg, m)

	if registryURL := kfg.RegistryConfig().RegistryURL; registryURL != "" {
		m.SetRegistryURL(registryURL)
		logger.Info().Str("registry", registryURL).Msg("registry URL configured from ORK_REGISTRY")
	}

	// ── 2. Scheme + Kubeclient ────────────────────────────────────────────────
	scheme, err := katalog.NewSchemeRegistry(kat)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to build scheme registry")
	}

	kube := kubeclient.NewKubeclient(kfg, scheme)
	if err := kube.Start(ctx); err != nil {
		logger.Fatal().Err(err).Msg("failed to start kubeclient")
	}

	// ── 3. HealthServer — HTTP ────────────────────────────────────────────────
	hs := health.NewHealthServer(kfg)

	// ── 4. Gateway API ────────────────────────────────────────────────────────
	clusters, clustersErr := apigateway.BuildClusterRegistry(ctx, kat, kube, kfg.Cluster().Namespace())
	if clustersErr != nil {
		logger.Fatal().Err(clustersErr).Msg("gateway cluster registry setup failed")
	}

	api, apiErr := apigateway.NewAPIServer(ctx, kat, kube, clusters, kfg.Cluster().Namespace())
	if apiErr != nil {
		logger.Fatal().Err(apiErr).Msg("gateway API setup failed")
	}

	intakeSrv, intakeErr := intake.NewIntakeServer(ctx, kat, kube, clusters, kfg.Cluster().Namespace())
	if intakeErr != nil {
		logger.Fatal().Err(intakeErr).Msg("gateway webhooks setup failed")
	}

	if api != nil {
		api.Register(hs)
		if intakeSrv != nil {
			intakeSrv.Register(hs, kat.Notes)
		}
	}

	// ── 5. Start ──────────────────────────────────────────────────────────────
	komponents := []domain.Komponent{hs, kube}

	o := ork.NewOrkestra(
		kfg.RunningInstance(),
		kfg.Katalog().ShutdownGracePeriod(),
		kfg.Ork().LogLevel(),
	)
	o.Register(komponents)

	go func() {
		if err := o.Start(ctx); err != nil {
			logger.Fatal().AnErr("gateway startup error", err)
			utils.Exit(err)
		}
	}()

	logger.Warn().Msg("local mode — admission and conversion webhooks are disabled (TLS not available)")
	logger.Info().Msg("gateway started — serving API on HTTP")

	o.Wait()
}
