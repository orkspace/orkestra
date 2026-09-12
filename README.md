# Orkestra Helm Chart

![Chart Version](https://img.shields.io/badge/chart%20version-0.7.17-blue?style=flat-square) ![App Version](https://img.shields.io/badge/app%20version-0.7.17-blue?style=flat-square)


Declarative Kubernetes Operator Runtime • Security-First • GitOps-Native

Orkestra is the **Kubernetes operator runtime** — operators without the infrastructure. This Helm chart deploys:

- **Orkestra Runtime** — the operator engine that reconciles your CRDs
- **Orkestra Gateway** — admission webhooks, deletion protection, namespace protection, and TLS — runs standalone or alongside the runtime
- **Orkestra Control Center** — multi-instance observability UI

```bash
helm repo add orkestra https://orkspace.github.io/orkestra
helm repo update
helm upgrade --install orkestra orkestra/orkestra \
  --namespace orkestra-system \
  --create-namespace
```

---

## Prerequisites

- Kubernetes 1.28+
- Helm 3.10+
- `kubectl` configured for your cluster
- `ork` CLI installed

```bash
curl -sSL https://get.orkestra.sh | bash
```

---

## Before you begin

Create a minimal Katalog (save as `katalog.yaml`):

```yaml
apiVersion: orkestra.orkspace.io/v1
kind: Katalog
metadata:
  name: my-platform
spec:
  crds:
    website:
      enabled: true
      apiTypes:
        group: demo.orkestra.io
        version: v1alpha1
        kind: Website
        plural: websites
      operatorBox:
        default: true
        onCreate:
          deployments:
            - image: nginx
              replicas: 1
```

If your CRD is not installed yet:

```bash
ork generate crd -f katalog.yaml -o website-crd.yaml
kubectl apply -f website-crd.yaml
```

---

## Security-First Installation (Recommended)

Orkestra generates **minimal RBAC** from your Katalog — no wildcards, no excess permissions. The full flow is five steps, starting with an offline review before anything touches the cluster.

### Step 1 — Preview permissions offline

```bash
ork validate --full
```

Shows every RBAC rule, named profile, and startup dependency your Katalog will request — per CRD, per component (runtime and gateway), with inline notes explaining why each permission exists. No cluster required.

Review this output before generating. What you see here is exactly what the bundle will contain.

### Step 2 — Generate a bundle

```bash
ork generate bundle -f katalog.yaml -o bundle.yaml
```

No cluster required. This produces a single YAML containing: a `Namespace`, `ServiceAccounts` for Runtime, Gateway, and Control Center, a `ClusterRole` with only the permissions your Katalog needs, a `ClusterRoleBinding`, and a `ConfigMap` with your Katalog data.

### Step 3 — Apply the bundle

```bash
kubectl apply -f bundle.yaml
```

### Step 4 — Deploy with Helm

```bash
helm upgrade --install orkestra orkestra/orkestra \
  --namespace orkestra-system \
  --create-namespace
```

The chart automatically uses the `ServiceAccount` and `ConfigMap` you just applied.

### Step 5 — Verify

```bash
kubectl get pods -n orkestra-system
kubectl get websites -A
```

> **Why this matters:** Traditional operators are massively over-permissioned. Orkestra generates RBAC from your declared intent — least-privilege security by default. `ork validate --full` makes that intent visible before a single resource is applied.

---

## Deployment Topologies

Orkestra's runtime and gateway are independent. Deploy what you need.

| Topology | `runtime.enabled` | `gateway.enabled` | Use case |
|----------|--------------------|-------------------|----------|
| Runtime only | `true` | `false` | Operators without admission control |
| **Gateway only** | `false` | `true` | Deletion protection, namespace protection — no operators required |
| Full split | `true` | `true` | Operators with admission webhooks and protection |

---

## Gateway Only — Deletion Protection Without an Operator

The gateway runs standalone. No operator, no CRDs, no reconcile loop required. Use it to protect any existing Kubernetes resources.

### Step 1 — Write a standalone Katalog

```yaml
# gateway-katalog.yaml
apiVersion: orkestra.orkspace.io/v1
kind: Katalog
metadata:
  name: platform-security
  author: platform-team

gateway:
  standalone: true

security:
  deletionProtection:
    enabled: true
    failurePolicy: Fail
    cleanupOnShutdown: false
  namespaceProtection:
    enabled: true
    failurePolicy: Fail
    restrictedNamespaces:
      - kube-system
      - kube-public
  certManager:
    autoRotate: true
    rotationThreshold: "30d"
    validFor: "2y"

spec: {}
```

`gateway.standalone: true` — no runtime required. No `spec.crds` needed.

### Step 2 — Generate and apply the bundle

```bash
ork generate bundle --for gateway -f gateway-katalog.yaml | kubectl apply -f -
```

### Step 3 — Install gateway only

```bash
helm upgrade --install orkestra orkestra/orkestra \
  --namespace orkestra-system \
  --create-namespace \
  --set runtime.enabled=false \
  --set controlCenter.enabled=false \
  --set gateway.enabled=true \
```

Label any resource `orkestra.io/deletion-protection: "true"` — the gateway intercepts every DELETE request for it at the API server.

---

## GitOps Workflow (Recommended)

In CI — all three steps run entirely offline, no cluster required:

```bash
ork validate --full -f katalog.yaml       # review permissions before generating
ork generate bundle -f katalog.yaml -n orkestra-system -o orkestra-bundle.yaml
# Commit orkestra-bundle.yaml to your GitOps repo
```

ArgoCD or Flux syncs the bundle (RBAC + ConfigMap) and the Helm release. This is the cleanest, most auditable way to run Orkestra.

---

## Runtime Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `runtime.enabled` | Deploy the runtime | `true` |
| `runtime.image.repository` | Runtime image | `ghcr.io/orkspace/orkestra` |
| `runtime.image.tag` | Image tag | Chart `appVersion` |
| `runtime.image.pullPolicy` | Pull policy | `IfNotPresent` |
| `runtime.replicaCount` | Replicas (leader election active) | `2` |
| `runtime.serviceAccount` | ServiceAccount name — must match generated RBAC | `"orkestra"` |
| `runtime.service.type` | Service type | `ClusterIP` |
| `runtime.service.port` | Service port | `8080` |
| `runtime.server.readTimeout` | HTTP read timeout (seconds) | `30` |
| `runtime.server.writeTimeout` | HTTP write timeout (seconds) | `60` |
| `runtime.config.logLevel` | Log level: debug, info, warn, error | `info` |
| `runtime.config.defaultWorkers` | Reconcile workers per CRD | `2` |
| `runtime.config.defaultResync` | Resync interval | `30s` |
| `runtime.config.maxDepth` | Max workqueue depth per CRD | `500` |
| `runtime.config.failureThreshold` | Failures before degraded state | `10` |
| `runtime.config.environment` | Deployment environment | `production` |
| `runtime.leaderElection.enabled` | HA leader election | `true` |
| `runtime.leaderElection.leaseDuration` | Lease duration (seconds) | `15` |
| `runtime.leaderElection.renewDeadline` | Renew deadline (seconds) | `10` |
| `runtime.leaderElection.retryPeriod` | Retry period (seconds) | `5` |
| `runtime.katalog.inline` | Inline Katalog YAML | starter Katalog |
| `runtime.katalog.existingConfigMap` | Use a pre-existing ConfigMap | `""` |
| `runtime.katalog.configMapKey` | Key inside the ConfigMap | `katalog.yaml` |
| `runtime.katalog.mountPath` | Mount path in container | `/etc/orkestra/katalog` |
| `runtime.gatewayEndpoint` | Gateway URL advertised on `/katalog` | `""` |
| `runtime.registry.enabled` | Enable OCI registry integration | `false` |
| `runtime.registry.url` | Registry URL | `""` |
| `runtime.resources` | CPU/memory requests and limits | see values.yaml |

---

## Gateway Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `gateway.enabled` | Deploy the gateway | `false` |
| `gateway.image.repository` | Gateway image | `ghcr.io/orkspace/orkestra-gateway` |
| `gateway.image.tag` | Image tag | Chart `appVersion` |
| `gateway.image.pullPolicy` | Pull policy | `IfNotPresent` |
| `gateway.replicaCount` | Replicas (stateless, no leader election) | `2` |
| `gateway.serviceAccount` | ServiceAccount name | `"orkestra-gateway"` |
| `gateway.service.type` | Service type | `ClusterIP` |
| `gateway.server.httpPort` | Health + metrics port | `8080` |
| `gateway.server.httpsPort` | Webhook (HTTPS) port | `8443` |
| `gateway.config.logLevel` | Log level | `info` |
| `gateway.webhooks.enabled` | Master switch for webhooks | `false` |
| `gateway.webhooks.admission` | Enable admission webhook | `false` |
| `gateway.webhooks.conversion` | Enable conversion webhook | `false` |
| `gateway.katalog.existingConfigMap` | ConfigMap containing the gateway Katalog | `""` |
| `gateway.katalog.configMapKey` | Key inside the ConfigMap | `katalog.yaml` |
| `gateway.resources` | CPU/memory requests and limits | see values.yaml |

---

## Control Center Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controlCenter.enabled` | Deploy the Control Center | `true` |
| `controlCenter.image.repository` | Control Center image | `ghcr.io/orkspace/orkestra-cc` |
| `controlCenter.replicaCount` | Replicas | `1` |
| `controlCenter.serviceAccount` | ServiceAccount name | `"orkestra-cc"` |
| `controlCenter.config.orkestraURLs` | Runtime URLs to monitor | `[]` |
| `controlCenter.config.refreshInterval` | Katalog refresh interval | `10s` |
| `controlCenter.config.logLevel` | Log level | `info` |
| `controlCenter.service.type` | Service type | `ClusterIP` |
| `controlCenter.service.port` | Service port | `8081` |
| `controlCenter.ingress.enabled` | Enable Ingress | `false` |
| `controlCenter.ingress.className` | Ingress class | `""` |
| `controlCenter.ingress.hosts` | Ingress hosts | `[]` |
| `controlCenter.ingress.tls` | TLS configuration | `[]` |

---

## Global Settings

| Parameter | Description | Default |
|-----------|-------------|---------|
| `global.namespace` | Namespace hint for all components | `orkestra-system` |
| `global.nameOverride` | Override chart name | `""` |
| `global.fullnameOverride` | Override full resource name | `""` |

---

## Per-Component Settings

All keys below apply independently under `runtime:`, `gateway:`, and `controlCenter:`.

| Parameter | Description | Default |
|-----------|-------------|---------|
| `<component>.imagePullSecrets` | Image pull secrets | `[]` |
| `<component>.hpa.enabled` | Enable HorizontalPodAutoscaler | `false` |
| `<component>.hpa.minReplicas` | HPA minimum replicas | `2` |
| `<component>.hpa.maxReplicas` | HPA maximum replicas | `5` |
| `<component>.hpa.targetCPUUtilizationPercentage` | HPA CPU target | `80` |
| `<component>.hpa.targetMemoryUtilizationPercentage` | HPA memory target | `80` |
| `<component>.networkPolicy.enabled` | Enable NetworkPolicy | `false` |
| `<component>.networkPolicy.ingressFrom` | Allowed ingress sources | `[]` |
| `<component>.pdb.enabled` | Enable PodDisruptionBudget | `true` |
| `<component>.pdb.minAvailable` | Minimum available pods | `1` |
| `<component>.nodeSelector` | Node selector | `{}` |
| `<component>.tolerations` | Tolerations | `[]` |
| `<component>.affinity` | Affinity rules | `{}` |
| `<component>.topologySpreadConstraints` | Topology spread constraints | `[]` |
| `<component>.extraEnv` | Extra environment variables | `[]` |
| `<component>.extraEnvFrom` | Extra env sources (ConfigMap/Secret) | `[]` |
| `<component>.extraVolumes` | Extra volumes | `[]` |
| `<component>.extraVolumeMounts` | Extra volume mounts | `[]` |
| `<component>.podAnnotations` | Pod annotations | `{}` |
| `<component>.podLabels` | Extra pod labels | `{}` |

---

## Production Example

```yaml
# production-values.yaml
runtime:
  replicaCount: 3
  serviceAccount: orkestra
  resources:
    requests:
      cpu: 200m
      memory: 256Mi
    limits:
      cpu: 1000m
      memory: 1Gi
  config:
    logLevel: warn
    defaultWorkers: 4
    defaultResync: 1m
  katalog:
    existingConfigMap: platform-katalog
  gatewayEndpoint: "http://orkestra-gateway.orkestra-system.svc:8080"
  hpa:
    enabled: true
    minReplicas: 3
    maxReplicas: 8
    targetCPUUtilizationPercentage: 70
  networkPolicy:
    enabled: true
    ingressFrom:
      - namespaceSelector:
          matchLabels:
            kubernetes.io/metadata.name: monitoring

gateway:
  enabled: true
  replicaCount: 2
  serviceAccount: orkestra-gateway
  katalog:
    existingConfigMap: platform-katalog
  hpa:
    enabled: true
    minReplicas: 2
    maxReplicas: 5

controlCenter:
  replicaCount: 2
  serviceAccount: orkestra-cc
  config:
    orkestraURLs:
      - http://orkestra-runtime:8080
    refreshInterval: 30s
  ingress:
    enabled: true
    className: nginx
    annotations:
      cert-manager.io/cluster-issuer: letsencrypt-prod
    hosts:
      - host: control-center.platform.myorg.io
        paths:
          - path: /
            pathType: Prefix
    tls:
      - secretName: control-center-tls
        hosts:
          - control-center.platform.myorg.io
  hpa:
    enabled: true
    minReplicas: 2
    maxReplicas: 5
```

```bash
helm upgrade --install orkestra orkestra/orkestra \
  --namespace orkestra-system --create-namespace \
  --values production-values.yaml
```

---

## Observability

```bash
ork proxy
```

```bash
# Runtime
curl localhost:8080/health
curl localhost:8080/metrics
curl localhost:8080/katalog | jq

# Gateway
curl localhost:8443/health
curl localhost:8443/metrics

# Control Center
open http://localhost:8081/controlcenter
```

Prometheus scrape configuration:

```yaml
- job_name: orkestra-runtime
  static_configs:
    - targets: ['orkestra-runtime.orkestra-system.svc.cluster.local:8080']

- job_name: orkestra-gateway
  static_configs:
    - targets: ['orkestra-gateway.orkestra-system.svc.cluster.local:8080']

- job_name: orkestra-control-center
  static_configs:
    - targets: ['orkestra-cc.orkestra-system.svc.cluster.local:8081']
```

---

## Upgrade

```bash
helm repo update
helm upgrade orkestra orkestra/orkestra \
  --namespace orkestra-system \
  --values values.yaml
```

---

## Uninstall

```bash
helm uninstall orkestra --namespace orkestra-system
```

Uninstalling removes Deployments and Services. CRDs and custom resources Orkestra was managing are **not** deleted — they remain in the cluster and must be cleaned up separately.

---

## Troubleshooting

**Control Center cannot connect to Runtime**

```bash
kubectl get svc -n orkestra-system
kubectl logs -n orkestra-system deployment/orkestra-cc
```

Ensure `controlCenter.config.orkestraURLs` points to the runtime service.

**Katalog not loaded**

```bash
kubectl get configmap -n orkestra-system
kubectl logs -n orkestra-system deployment/orkestra-runtime | grep -i katalog
```

**RBAC permission denied**

```bash
ork generate bundle -f katalog.yaml -o bundle.yaml
kubectl apply -f bundle.yaml
```

**Webhooks not working**

```bash
kubectl get validatingwebhookconfigurations
kubectl logs -n orkestra-system deployment/orkestra-gateway | grep -i webhook
```

---

## Security Posture

### Minimal production binaries

Two binaries, two build tags, two attack surfaces:

| Command | Developer CLI | Runtime binary | Gateway binary |
|---------|:---:|:---:|:---:|
| `ork run` | ✓ | ✓ | — |
| `ork gate` | — | — | ✓ |
| `ork version` | ✓ | ✓ | ✓ |
| `ork generate` | ✓ | — | — |
| `ork validate` | ✓ | — | — |
| `ork init` | ✓ | — | — |

A compromised runtime container cannot serve webhooks. A compromised gateway container cannot generate RBAC bundles or enumerate CRDs. Each binary does exactly one thing.

### Explicit, auditable RBAC

Orkestra never auto-creates `ServiceAccount`, `ClusterRole`, or `ClusterRoleBinding` resources. You generate them from your Katalog, review the output, commit it, and apply it explicitly. Every permission is visible in source control before it reaches the cluster.

### Deletion protection

Label any resource `orkestra.io/deletion-protection: "true"`. The gateway intercepts every DELETE request at the API server before it reaches etcd. The webhook configuration is self-healing — if deleted, the housekeeper recreates it within the configured sync interval.

### TLS — automatic rotation

The gateway generates its own TLS certificate and rotates it pre-emptively before expiry. Configure via the Katalog:

```yaml
security:
  certManager:
    autoRotate: true
    rotationThreshold: "30d"
    validFor: "2y"
```

To supply your own certificate: set `TLS_CERT` and `TLS_KEY` in the gateway environment — Orkestra will not touch the cert lifecycle.

---

## Why Security-First?

| Traditional operators | Orkestra |
|----------------------|----------|
| Wildcard permissions (`*/*/*`) | Minimal, derived from your Katalog |
| RBAC written by hand | Generated automatically |
| Drifts over time | Always in sync |
| Over-permissioned by default | Least privilege by default |
| One binary does everything | Runtime runs, Gateway gates |

---

## License

Same license as the Orkestra project.
