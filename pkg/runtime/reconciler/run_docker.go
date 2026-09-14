// pkg/reconciler/run_docker.go
//
// Docker build/push dispatch — runs before resource groups in runTemplateReconcile.
//
// The Docker hook performs a minimal docker build and optional push of the
// declared image. Results are injected into the resolver context via
// resolver.WithDocker() so subsequent template expressions and when: conditions
// can reference them.
//
// Call sequence in runTemplateReconcile:
//
//  1. resolver = NewResolver(ctx, obj)
//  2. resolver = resolver.WithCross(ReadCross(...))     ← cross-CRD first
//  3. resolver, err = runGit(ctx, ...)                  ← Git second
//  4. resolver, err = runExternal(ctx, ...)             ← HTTP third
//  5. resolver, err = runDocker(ctx, ...)               ← Docker fourth
//  6. runDeployments, runServices, ... etc.
//
// Results in template context:
//
//	.docker.image           → built image reference
//	.docker.buildSucceeded  → "true" or "false"
//	.docker.error           → error message (if any)
//	.docker.called          → "true" if hook executed
//
// When: conditions can gate on these:
//
//	when:
//	  - field: docker.buildSucceeded
//	    equals: "true"
package reconciler

import (
	"context"
	"fmt"
	"time"

	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/metrics"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// runDocker executes the declared Docker hook and returns a new resolver with
// .docker.* injected.
//
// Returns the enriched resolver. The original resolver is unchanged.
// Returns an error only if template resolution fails. Docker build/push errors
// are injected into .docker.error and do not stop reconciliation.
func runDocker(
	ctx context.Context,
	gvk string,
	resolver *orktmpl.Resolver,
	spec *orktypes.DockerHookSpec,
) (*orktmpl.Resolver, error) {

	if spec == nil {
		return resolver, nil
	}

	log := logger.FromContext(ctx)

	// ───────────────────────────────────────────────────────────────
	// 1. Resolve template expressions
	// ───────────────────────────────────────────────────────────────
	image, err := resolver.Resolve(spec.Image)
	if err != nil {
		return resolver, fmt.Errorf("docker.image: %w", err)
	}

	wd, err := resolver.Resolve(spec.WorkingDirectory)
	if err != nil {
		return resolver, fmt.Errorf("docker.workingDirectory: %w", err)
	}

	dockerfile, err := resolver.Resolve(spec.Dockerfile)
	if err != nil {
		return resolver, fmt.Errorf("docker.dockerfile: %w", err)
	}
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	builder, err := resolver.Resolve(spec.Builder)
	if err != nil {
		return resolver, fmt.Errorf("docker.builder: %w", err)
	}

	log.Debug().
		Str("image", image).
		Str("workingDirectory", wd).
		Str("dockerfile", dockerfile).
		Str("builder", builder).
		Msg("docker: starting build")

	// ───────────────────────────────────────────────────────────────
	// 2. Execute build
	// ───────────────────────────────────────────────────────────────
	start := time.Now()
	buildResult := executeDockerBuild(ctx, wd, dockerfile, image, builder)
	buildDuration := time.Since(start).Seconds()

	metrics.RecordDockerOperation(gvk, image, "build", buildResult.Error, buildDuration)

	if buildResult.Error != "" {
		log.Warn().
			Str("image", image).
			Str("workingDirectory", wd).
			Str("error", buildResult.Error).
			Msg("docker: build failed")

		if !spec.ContinueOnError {
			return resolver, fmt.Errorf("docker: %s", buildResult.Error)
		}
	} else {
		log.Debug().
			Str("image", image).
			Str("workingDirectory", wd).
			Msg("docker: build succeeded")
	}

	// ───────────────────────────────────────────────────────────────
	// 3. Optionally push (kaniko pushes during build — skip for it)
	// ───────────────────────────────────────────────────────────────
	if spec.Push && buildResult.Error == "" {
		log.Debug().
			Str("image", image).
			Msg("docker: pushing image")

		startPush := time.Now()
		pushResult := executeDockerPush(ctx, image, builder)
		pushDuration := time.Since(startPush).Seconds()

		metrics.RecordDockerOperation(gvk, image, "push", pushResult.Error, pushDuration)

		if pushResult.Error != "" {
			log.Warn().
				Str("image", image).
				Str("error", pushResult.Error).
				Msg("docker: push failed")
			buildResult.Error = pushResult.Error

			if !spec.ContinueOnError {
				return resolver, fmt.Errorf("docker push: %s", pushResult.Error)
			}
		} else {
			log.Debug().
				Str("image", image).
				Msg("docker: push succeeded")
		}
	}

	// ───────────────────────────────────────────────────────────────
	// 4. Inject results into resolver
	// ───────────────────────────────────────────────────────────────
	buildSucceeded := "false"
	if buildResult.Error == "" {
		buildSucceeded = "true"
	}

	dockerMap := map[string]interface{}{
		"image":          image,
		"buildSucceeded": buildSucceeded,
		"error":          buildResult.Error,
		"called":         "true",
	}

	resolver = resolver.WithDocker(dockerMap)
	return resolver, nil
}
