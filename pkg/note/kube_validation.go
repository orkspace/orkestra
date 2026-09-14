package note

import (
	"text/template"

	"k8s.io/apimachinery/pkg/api/validate/content"
)

// Kubernetes-standard label/annotation format checks, exposed as notes.
//
// Kubernetes already validates this format at the API server: a label or
// annotation key must be a valid [prefix/]name, and a label value (unlike
// an annotation value, which is an unrestricted free-form string) follows
// that same name format. ork validate already runs this exact check once,
// at katalog-authoring time, against the keys a katalog author declares
// under serve.labels and serve.annotations — but nothing checks the values
// a runtime client actually submits (curl, raw kubectl, a custom UI) until
// the object reaches the real API server.
//
// These notes expose that same upstream Kubernetes check so a
// validation.rules entry can gate on it directly, at admission time, with
// an Orkestra-native message instead of the API server's raw rejection —
// and so ork simulate, which doesn't run full structural schema
// validation, can catch it too.
//
// Usage examples:
//   {{ isValidLabelValue (getLabel . "team") }}
//   {{ isValidLabelKey "platform.myorg.io/tier" }}
//   {{ isValidAnnotationKey "platform.myorg.io/jira-ticket" }}
//   {{ isDNS1123Subdomain .spec.hostname }}

func kubeValidationNotes() template.FuncMap {
	return template.FuncMap{
		"isValidLabelValue":    noteIsValidLabelValue,
		"isValidLabelKey":      noteIsValidLabelKey,
		"isValidAnnotationKey": noteIsValidAnnotationKey,
		"isDNS1123Subdomain":   noteIsDNS1123Subdomain,
	}
}

// noteIsValidLabelValue reports whether value is a syntactically valid
// Kubernetes label value: max 63 characters, alphanumeric, may contain '-',
// '_', '.', and must start and end with an alphanumeric character. An empty
// string is valid — Kubernetes treats an absent label value as permitted.
//
//	{{ isValidLabelValue (getLabel . "team") }}
func noteIsValidLabelValue(value string) bool {
	return len(content.IsLabelValue(value)) == 0
}

// noteIsValidLabelKey reports whether key is a syntactically valid
// Kubernetes label key: [prefix/]name — name is alphanumeric (max 63
// chars, may contain '-', '_', '.'), and prefix, if present, must be a
// valid DNS subdomain (max 253 chars).
//
//	{{ isValidLabelKey "platform.myorg.io/tier" }}
func noteIsValidLabelKey(key string) bool {
	return len(content.IsLabelKey(key)) == 0
}

// noteIsValidAnnotationKey reports whether key is a syntactically valid
// Kubernetes annotation key — the same [prefix/]name format as a label key.
// Annotation values have no Kubernetes format restriction of their own
// (that's what distinguishes them from labels), so there is no
// isValidAnnotationValue note.
//
//	{{ isValidAnnotationKey "platform.myorg.io/jira-ticket" }}
func noteIsValidAnnotationKey(key string) bool {
	return len(content.IsLabelKey(key)) == 0
}

// noteIsDNS1123Subdomain reports whether value is a syntactically valid
// DNS-1123 subdomain: lowercase alphanumeric segments (may contain '-')
// separated by '.', max 253 characters. This is the format Kubernetes
// requires for object names in most resource types, and for the prefix
// portion of a label/annotation key.
//
//	{{ isDNS1123Subdomain .spec.hostname }}
func noteIsDNS1123Subdomain(value string) bool {
	return len(content.IsDNS1123Subdomain(value)) == 0
}
