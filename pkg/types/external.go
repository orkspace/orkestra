// pkg/types/external.go
//
// External call declarations — HTTP today, multi-protocol via protocol:.
//
// The external: block allows declarative calls before resource reconciliation.
// Results are injected into the resolver context under .external.<name> and
// available in subsequent template expressions and when: conditions.
//
// YAML:
//
//	onReconcile:
//	  external:
//	    - name: health-check
//	      url: "{{ .spec.serviceUrl }}/health"
//	      method: GET
//	      expectedStatus: 200
//	      continueOnError: false
//	      timeout: 5s
//
//	    - name: feature-flags
//	      url: "https://flags.internal/api/{{ .metadata.name }}"
//	      method: GET
//	      auth:
//	        env: FEATURE_FLAG_TOKEN
//	      continueOnError: true
//
//	    - name: queueDepth
//	      protocol: prometheus
//	      url: "http://prometheus.monitoring:9090"
//	      query: 'sum(rabbitmq_queue_messages{queue="build-queue"})'
//	      cacheFor: 30s
//
//	  deployments:
//	    - name: "{{ .metadata.name }}"
//	      image: "{{ .spec.image }}"
//	      when:
//	        - field: external.health-check.status
//	          equals: "200"
//	        - field: "{{ promAboveThreshold .external.queueDepth 1000 }}"
//	          equals: "false"
//
// Calls run sequentially in declaration order. Each call can reference earlier
// calls' results in its own url:, query:, and body: template expressions.
//
// Security: use auth.secretRef to read credentials from a Kubernetes Secret,
// or auth.env to read from a pod environment variable. Never put raw secrets
// in Katalog YAML.
package types

// FiresConfig controls at which lifecycle points an external call is executed.
// When absent, the call fires at both admission and reconcile time (the safe default).
type FiresConfig struct {
	// Reconcile controls whether the call fires at reconcile time.
	// Default true. Set false for admission-only calls (e.g. image signing checks)
	// where the admission check is trusted and re-running every reconcile is wasteful.
	Reconcile *bool `yaml:"reconcile,omitempty" json:"reconcile,omitempty"`
}

// FiresAtReconcile reports whether this FiresConfig allows reconcile-time firing.
func (f *FiresConfig) FiresAtReconcile() bool {
	if f == nil || f.Reconcile == nil {
		return true
	}
	return *f.Reconcile
}

// ExternalProtocol identifies the wire protocol for an external call.
// Default (empty string) is HTTP — identical to pre-protocol behaviour.
type ExternalProtocol string

const (
	ProtocolHTTP       ExternalProtocol = "http"
	ProtocolRedis      ExternalProtocol = "redis"
	ProtocolPrometheus ExternalProtocol = "prometheus"
	ProtocolPostgres   ExternalProtocol = "postgres"
	ProtocolMongo      ExternalProtocol = "mongo"
	ProtocolGRPC       ExternalProtocol = "grpc"
	ProtocolKafka      ExternalProtocol = "kafka"
	ProtocolNATS       ExternalProtocol = "nats"
	ProtocolMQTT       ExternalProtocol = "mqtt"
)

// ExternalAuth declares how to resolve a credential for an external call.
// Exactly one of SecretRef or Env must be set.
type ExternalAuth struct {
	// SecretRef reads the credential from a Kubernetes Secret.
	// Namespace defaults to the operator's own namespace if omitted.
	// Read via the API server watch cache — no etcd round-trip.
	SecretRef *APISecretRef `yaml:"secretRef,omitempty" json:"secretRef,omitempty"`

	// Env reads the credential from a pod environment variable.
	// Value is the variable name without $: env: MY_TOKEN reads $MY_TOKEN.
	Env string `yaml:"env,omitempty" json:"env,omitempty"`

	// Header is the HTTP header the credential is injected into.
	// Default: "Authorization" (produces "Bearer <value>").
	// Set to "X-Api-Key" or similar for non-Bearer schemes.
	// HTTP only — ignored by stateful protocol clients.
	Header string `yaml:"header,omitempty" json:"header,omitempty"`
}

// Deprecated ExternalSecretRef locates a Kubernetes Secret that holds a credential.
type ExternalSecretRef struct {
	// Name is the Secret name.
	Name string `yaml:"name" json:"name"`
	// Namespace is the Secret namespace. Defaults to the operator's own namespace.
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	// Key is the data key within the Secret.
	Key string `yaml:"key" json:"key"`
}

// ExternalCallSpec declares one external call to make before resource reconciliation.
type ExternalCallSpec struct {
	// Name is the identifier used to access the result in template expressions.
	//   name: health-check → {{ .external.health-check.status }}
	Name string `yaml:"name" json:"name"`

	// Protocol identifies the wire protocol. Default (Empty(): HTTP.
	//   protocol: prometheus
	//   protocol: redis
	//   protocol: postgres
	Protocol ExternalProtocol `yaml:"protocol,omitempty" json:"protocol,omitempty"`

	// URL is the endpoint or connection string. Template expressions supported.
	//   url: "{{ .spec.serviceUrl }}/health"
	//   url: "redis://redis-svc:6379"
	URL string `yaml:"url" json:"url"`

	// Query is a protocol-specific instruction executed after connection.
	// HTTP: unused (use body: for request body).
	// Prometheus: PromQL expression.
	// Postgres: SQL query string.
	// Redis: command and args — "GET mykey", "HGET hash field".
	// Kafka: "consumer-group/topic" or "@topic" for topic metadata.
	// NATS: "bucket.key" for KV; stream name for stream info.
	// MQTT: retained topic to read.
	Query string `yaml:"query,omitempty" json:"query,omitempty"`

	// CacheFor gates re-fetch. Calls within this window return the cached result.
	// Empty: fetch every reconcile. Recommended for stateful protocols where
	// connection establishment is expensive.
	CacheFor string `yaml:"cacheFor,omitempty" json:"cacheFor,omitempty"`

	// Auth declares the credential for this call.
	// For HTTP this replaces the deprecated token: field.
	// For non-HTTP protocols, secretRef or env must be set if authentication
	// is required by the target.
	Auth *ExternalAuth `yaml:"auth,omitempty" json:"auth,omitempty"`

	// PoolSize is the maximum connections maintained per url:.
	// Only meaningful for stateful protocols (postgres, kafka, redis, nats, mqtt).
	// Default: 2.
	PoolSize int `yaml:"poolSize,omitempty" json:"poolSize,omitempty"`

	// Method is the HTTP method. Default: GET. HTTP only.
	Method string `yaml:"method,omitempty" json:"method,omitempty"`

	// Body is the request body for POST/PUT/PATCH. Template expressions supported.
	//   body: '{"name": "{{ .metadata.name }}"}'
	// HTTP only.
	Body string `yaml:"body,omitempty" json:"body,omitempty"`

	// Headers are additional HTTP headers to include. HTTP only.
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`

	// Timeout is the maximum duration for this call.
	// Default: 10s. Format: "5s", "1m", "500ms"
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty"`

	// ExpectedStatus is the HTTP status code that signals success. HTTP only.
	// Default: any 2xx status.
	ExpectedStatus int `yaml:"expectedStatus,omitempty" json:"expectedStatus,omitempty"`

	// ContinueOnError controls whether a failed call halts reconciliation.
	// false (default): failure returns an error, halting the reconcile.
	// true:            failure is logged, result has error set, reconcile continues.
	ContinueOnError bool `yaml:"continueOnError,omitempty" json:"continueOnError,omitempty"`

	// When conditions gate this call — skipped if conditions fail.
	Conditions []Condition `yaml:"when,omitempty" json:"when,omitempty"`
	Or         []Condition `yaml:"or,omitempty" json:"or,omitempty"`

	// Sleep injects an artificial delay. Useful for testing.
	// Accepts extended duration units (s, m, h, d, w, mo, y).
	Sleep string `json:"sleep,omitempty" yaml:"sleep,omitempty"`

	// Fires controls at which lifecycle points this call executes.
	// Absent: fires at both admission and reconcile time.
	// fires.reconcile: false — admission only; reconciler skips this call.
	Fires *FiresConfig `yaml:"fires,omitempty" json:"fires,omitempty"`

	// Include is a path to a YAML file containing a top-level "calls:" list.
	// When set, this entry is replaced in-place by the listed calls.
	// Resolved relative to the katalog file's directory. Cleared after expansion.
	Include string `yaml:"include,omitempty" json:"include,omitempty"`

	// RetryBackoff configures how many times and how long to wait between
	// retries for this specific external call before returning an error.
	// Shorthand ("3s") sets initial only; full form gives full control.
	RetryBackoff *RetryBackoffConfig `yaml:"retryBackoff,omitempty" json:"retryBackoff,omitempty"`
}

// ExternalCallResult is the result of one HTTP call, injected into the resolver
// context under .external.<name>.
type ExternalCallResult struct {
	// Status is the HTTP status code as a string ("200", "404", "503").
	// Empty string when the call failed before receiving a response.
	Status string `json:"status" yaml:"status"`

	// Body is the first 4096 bytes of the response body.
	// Truncated to avoid unbounded memory use.
	Body string `json:"body" yaml:"body"`

	// Error is the error message when the call failed.
	// Empty on success.
	Error string `json:"error" yaml:"error"`

	// Called is "true" when the call was made, "false" when skipped (conditions failed).
	Called string `json:"called" yaml:"called"`

	// Additional values for metrics
	StatusCode      int     `json:"statusCode" yaml:"statusCode"`
	DurationSeconds float64 `json:"durationSeconds" yaml:"durationSeconds"`
}

// hasSecreRef is a generic helper that
func (e *ExternalCallSpec) HasSecretRef() bool {
	return e.Auth != nil && e.Auth.SecretRef != nil
}
