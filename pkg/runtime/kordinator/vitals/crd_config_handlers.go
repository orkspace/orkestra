package vitals

import (
	"fmt"
	"net/http"

	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/merger"
	"github.com/orkspace/orkestra/pkg/utils"
)

// ─────────────────────────────────────────────────────────────────────────────
// CRD Raw Handler
// Returns a single CRD definition from the merged Katalog as JSON.
// This endpoint powers the "View Source" button in the CRD detail view,
// allowing users to see exactly how the CRD was defined in the Katalog.
//
// Endpoint: /katalog/{crd}/raw
//
// Example response:
//
//	{
//	  "name": "website",
//	  "apiTypes": {
//	    "group": "demo.orkestra.io",
//	    "version": "v1alpha1",
//	    "kind": "Website",
//	    "plural": "websites"
//	  },
//	  "operatorBox:": {
//	    "onCreate": {
//	      "deployments": [
//	        {
//	          "image": "{{ .spec.image }}"
//	        }
//	      ]
//	    }
//	  }
//	}
//
// ─────────────────────────────────────────────────────────────────────────────
func BuildCRDRawHandler(m *merger.Merger, crdName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		crd, ok := m.Get(crdName)
		if !ok {
			utils.WriteJSON(w, http.StatusNotFound, map[string]interface{}{
				"error": fmt.Sprintf("CRD %q not found in Katalog", crdName),
			})
			return
		}

		// Use the pruned writer
		utils.WriteJSONPruned(w, http.StatusOK, crd)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Raw Katalog Handler
// Returns the complete merged Katalog as JSON, including metadata and all CRDs.
// This endpoint is used by the Control Center to display the source Katalog
// that defines the current operator.
//
// Endpoint: /katalog/raw
//
// The response contains:
//   - apiVersion: The Orkestra API version
//   - kind: Always "Katalog" at runtime (merged from sources)
//   - metadata: Katalog metadata (name, description, version, author, license)
//   - spec.crds: All merged CRD definitions from all sources
//
// Example response:
//
//	{
//	  "apiVersion": "orkestra.orkspace.io/v1",
//	  "kind": "Katalog",
//	  "metadata": {
//	    "name": "platform-katalog",
//	    "description": "Production operator suite",
//	    "version": "1.0.0",
//	    "author": "Platform Team"
//	  },
//	  "spec": {
//	    "crds": [
//	      {
//	        "name": "website",
//	        "apiTypes": { ... },
//	        "operatorBox:": { ... }
//	      }
//	    ]
//	  }
//	}
//
// ─────────────────────────────────────────────────────────────────────────────
func BuildRawKatalogHandler(m *merger.Merger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uiKatalog := m.ToUI()

		// Use the pruned writer to remove all null/empty values
		utils.WriteJSONPruned(w, http.StatusOK, uiKatalog)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CRD Enriched Handler
// Returns a single CRD definition with all enriched/default values applied.
// This endpoint powers the "View Enriched" button in the CRD detail view,
// allowing users to see exactly what Orkestra uses at runtime after applying
// default workers, default resync, enriched API types, and resolved dependencies.
//
// The enriched view shows:
//   - Fully resolved API types (group, version, plural enriched from discovery API)
//   - Default workers applied when not specified (from DEFAULT_WORKERS env var)
//   - Default resync applied when not specified (from DEFAULT_RESYNC env var)
//   - Default queue depth applied when not specified (from QUEUE_DEPTH env var)
//   - All validation and mutation rules merged from sources
//   - Complete operatorBox: configuration after inheritance
//
// Endpoint: /katalog/{crd}/enriched
//
// Example response (user wrote only `kind: Service`):
//
//	{
//	  "name": "service-watcher",
//	  "apiTypes": {
//	    "group": "",
//	    "version": "v1",
//	    "kind": "Service",
//	    "plural": "services"
//	  },
//	  "workers": 3,
//	  "resync": "30s",
//	  "maxDepth": 2000,
//	  "dependsOn": {
//	    "pod-manager": {
//	      "condition": "started"
//	    }
//	  }
//	}
//
// This shows what Orkestra actually uses at runtime versus what the user wrote.
// ─────────────────────────────────────────────────────────────────────────────
func BuildCRDEnrichedHandler(kat *katalog.Katalog, crdName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get the enriched CRD from the validated Katalog
		crd, err := kat.Get(crdName)
		if err != nil {
			utils.WriteJSON(w, http.StatusNotFound, map[string]interface{}{
				"error": fmt.Sprintf("CRD %q not found in enriched Katalog", crdName),
			})
			return
		}

		// Use the pruned writer to remove all null/empty values
		utils.WriteJSONPruned(w, http.StatusOK, crd)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Enriched Katalog Handler
// Returns the complete enriched Katalog as JSON, including metadata and all CRDs
// with all default values and enrichments applied. This endpoint shows what
// Orkestra actually uses at runtime after processing the user's Katalog.
//
// Endpoint: /katalog/enriched
//
// The response contains the same structure as /katalog/raw, but with:
//   - All defaults applied (workers, resync, queue depth)
//   - All API types enriched (group, version, plural from discovery API)
//   - All validation and mutation rules merged
//   - Complete operatorBox: configuration after inheritance
//   - Resolved dependencies with conditions
//
// This endpoint is useful for:
//   - Understanding what Orkestra actually does with your Katalog
//   - Debugging why certain values are being used at runtime
//   - Seeing the full effect of inheritance and defaults
//   - Comparing user intent with runtime reality
//
// Example response (user wrote minimal Katalog with just `kind: Pod`):
//
//	{
//	  "apiVersion": "orkestra.orkspace.io/v1",
//	  "kind": "Katalog",
//	  "metadata": {
//	    "name": "platform-katalog",
//	    "description": "Production operator suite"
//	  },
//	  "spec": {
//	    "crds": [
//	      {
//	        "name": "pod-manager",
//	        "apiTypes": {
//	          "group": "",
//	          "version": "v1",
//	          "kind": "Pod",
//	          "plural": "pods"
//	        },
//	        "workers": 3,
//	        "resync": "30s",
//	        "maxDepth": 2000,
//	        "failureThreshold": 10,
//	        "namespaced": true,
//	        "operatorBox:": {
//	          "default": true
//	        }
//	      }
//	    ]
//	  }
//	}
//
// ─────────────────────────────────────────────────────────────────────────────
func BuildEnrichedKatalogHandler(kat *katalog.Katalog) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Use the pruned writer to remove all null/empty values
		utils.WriteJSONPruned(w, http.StatusOK, kat.ToUI())
	}
}
