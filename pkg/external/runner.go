// Package external executes declarative external calls and injects results
// into the resolver context under .external.<name>.
// Used by both the reconciler (at reconcile time) and the gateway webhook (at admission time).
package external

import (
	"context"
	"fmt"

	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/metrics"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils"
	"k8s.io/client-go/kubernetes"
)

// Run executes all declared external calls sequentially and returns a new
// resolver with results injected under .external.<name>.
//
// Calls run in declaration order. Each call can reference earlier calls'
// results in its own url:, query:, and body: template expressions.
//
// gvk is the CRD identifier used for metrics labelling and cache keying.
// cs is used to resolve auth.secretRef — pass nil when secretRef is not used.
// Returns the enriched resolver. The original is unchanged.
// Returns an error only when continueOnError is false and a call fails.
func Run(
	ctx context.Context,
	gvk string,
	resolver *orktmpl.Resolver,
	calls []orktypes.ExternalCallSpec,
	cs kubernetes.Interface,
) (*orktmpl.Resolver, error) {
	if len(calls) == 0 {
		return resolver, nil
	}

	log := logger.FromContext(ctx)
	results := make(map[string]interface{}, len(calls))

	for i, call := range calls {
		if !orktypes.EvaluateConditions(resolver.Data(), call.Conditions, call.Or, resolver.TemplateEvaluator()) {
			results[call.Name] = skippedResult(call.Protocol)
			log.Debug().
				Str("call", call.Name).
				Int("index", i).
				Msg("external call skipped — when: conditions not met")
			continue
		}

		resolvedURL, err := resolver.Resolve(call.URL)
		if err != nil {
			return resolver, fmt.Errorf("external[%d].url: %w", i, err)
		}
		resolvedQuery, err := resolver.Resolve(call.Query)
		if err != nil {
			return resolver, fmt.Errorf("external[%d].query: %w", i, err)
		}
		resolvedBody, err := resolver.Resolve(call.Body)
		if err != nil {
			return resolver, fmt.Errorf("external[%d].body: %w", i, err)
		}
		credential, _, err := resolveAuth(ctx, call.Auth, cs)
		if err != nil {
			return resolver, fmt.Errorf("external[%d] %q: %w", i, call.Name, err)
		}

		// Cache check for protocols that declare cacheFor:.
		var entry map[string]interface{}
		cacheHit := false
		if call.CacheFor != "" {
			key := cacheKey(gvk, call.Name, resolvedURL, resolvedQuery)
			if cached, ok := cacheGet(key); ok {
				entry = cached
				cacheHit = true
				log.Debug().Str("call", call.Name).Msg("external call: cache hit")
			}
		}

		if !cacheHit {
			client := newProtocolClient(call.Protocol)
			entry, err = client.Fetch(ctx, call, resolvedURL, resolvedQuery, resolvedBody, credential)
			if err != nil {
				// Hard error from the client (e.g. context cancelled).
				if !call.ContinueOnError {
					return resolver, fmt.Errorf("external call %q: %w", call.Name, err)
				}
				log.Warn().Err(err).Str("call", call.Name).Msg("external call hard error — continuing")
				entry = errorResult(err.Error())
			}

			if call.CacheFor != "" {
				if ttl, err := utils.ParseTimeDuration(call.CacheFor); err == nil {
					key := cacheKey(gvk, call.Name, resolvedURL, resolvedQuery)
					cacheSet(key, entry, ttl)
				}
			}
		}

		results[call.Name] = entry

		// Record metrics using the url for HTTP calls; query for others.
		callErr, _ := entry["error"].(string)
		statusStr, _ := entry["status"].(string)
		durationSeconds := 0.0 // non-HTTP clients don't surface duration yet
		metrics.RecordExternalCall(gvk, call.Name, resolvedURL, durationSeconds, callErr, 0)

		// Inject before any early return so status fields can reference this
		// call's result even when continueOnError is false.
		resolver = resolver.WithExternal(results)

		if callErr != "" {
			log.Warn().
				Str("call", call.Name).
				Str("url", resolvedURL).
				Str("error", callErr).
				Msg("external call failed")

			if !call.ContinueOnError {
				return resolver, fmt.Errorf("external call %q failed: %s", call.Name, callErr)
			}
		} else {
			log.Debug().
				Str("call", call.Name).
				Str("url", resolvedURL).
				Str("status", statusStr).
				Msg("external call succeeded")
		}
	}

	return resolver, nil
}

// skippedResult returns the zero-value result for a skipped call.
// HTTP uses status/body/error/called; non-HTTP uses result/raw/error/called.
func skippedResult(protocol orktypes.ExternalProtocol) map[string]interface{} {
	switch protocol {
	case "", orktypes.ProtocolHTTP:
		return map[string]interface{}{
			"status": "",
			"body":   "",
			"error":  "",
			"called": "false",
		}
	default:
		return map[string]interface{}{
			"result": "",
			"raw":    map[string]interface{}{},
			"error":  "",
			"called": "false",
		}
	}
}
