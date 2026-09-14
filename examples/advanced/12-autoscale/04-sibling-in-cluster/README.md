# 04 — Autoscale: Sibling‑in‑Cluster

This example builds on **[03-sibling-in-binary](../03-sibling-in-binary/README.md)**.  
It reuses the same CRDs and workload logic, but runs **two independent Orkestra runtimes** in **two namespaces** inside the same cluster.

This demonstrates:

- **Cross‑binary autoscaling**  
- **Cross‑namespace autoscaling**  
- **Cross‑operator metric sharing**  
- **Multi‑runtime Control Center aggregation**

---

## What You Learn

- How multi‑runtime aggregation works in the Control Center  
- How **autoscale profiles** expand into full autoscale specs  
- How **multiple CRDs in separate binaries** autoscale independently  
- How to use **cross.\<crd\>.metrics.\*** to read sibling metrics across binaries  
- How **profile‑based autoscaling** differs from **explicit autoscaling**  
- How downstream CRDs (Processor, Auditor) react differently to upstream Loader pressure  
- How Orkestra performs **cross‑runtime dependency scaling** with no restarts, no redeployments, and no external metrics  
- How the Control Center visualizes **baseline vs override** across multiple runtimes  

---

## Prerequisites

- Ork CLI  
- Kubernetes cluster (Kind recommended)

Install Ork CLI:

```bash
curl get.orkestra.sh | bash
```

Create the Namespaces:

```bash
kubectl create namespace loader-system
kubectl create namespace processor-system
```

---

## Runtimes

Run the first runtime:

```bash
ORK_NAMESPACE=loader-system ork run -f katalog-loader.yaml
```

This:

- runs Orkestra in namespace `loader-system`
- exposes Control Center on port `8080`

Run the second runtime:

```bash
ORK_NAMESPACE=processor-system ORK_PORT=8090 ork run -f katalog-processor.yaml
```

This runs a second Orkestra instance in `processor-system` on port `8090`.

Start the Control Center:

```bash
ork control -u localhost:8080,localhost:8090
# username:password → orkestra
```

This registers **both runtimes** at startup.

Visit: **http://localhost:8081**

---

## Run the Example

### 1. Apply the CRDs

```bash
kubectl apply -f crd-loader.yaml
kubectl apply -f crd-processor.yaml
```

### 2. Apply the CRs

```bash
kubectl apply -f cr-loader.yaml
kubectl apply -f cr-processor.yaml
```

In the Control Center, you will see two runtimes:

- `loader-autoscale-sibling-in-cluster`
- `processor-autoscale-sibling-in-cluster`

Open each in separate tabs to watch:

- Loader generating work  
- Processor autoscaling based on Loader’s queue depth  

> **Note:**  
> Loader (in `loader-system`) has **no autoscaler**.  
> Processor (in `processor-system`) autoscale **based on Loader’s queue depth** via `cross.loader.metrics.queueDepth`.

---

## Load the Loader (Cross‑Binary Autoscale)

```bash
./load.sh loader up 100
```

Observe:

- Loader queue grows  
- Once queue hits **60% of Processor’s override queueDepth** for the full **5s interval**, Processor autoscaler activates  
- Processor workers scale **2 → 8**  
- Processor queueDepth scales **100 → 1000**  

### Expected logs in `processor-system`

```json
{"level":"info","crd":"autoscale.orkestra.io/v1alpha1, Kind=Processor","workers":8,"message":"autoscaler: worker pool resized"}
{"level":"info","crd":"autoscale.orkestra.io/v1alpha1, Kind=Processor","queueDepth":1000,"message":"autoscaler: queue depth limit updated"}
```

This is **cross‑binary autoscaling** using the standard Orkestra metrics API.

### What Processor is reading:

```json
// curl -sSL http://localhost:8080/katalog/loader | jq .metrics
{
  "errorRatePercent": 0,
  "queueDepth": 98,
  "reconcileDurationP95Ms": 1244.18,
  "workersBusyPercent": 100,
  "workersIdlePercent": 0
}
```

Processor sees Loader’s queue depth **live**, across namespaces, across binaries.

---

## Reduce the Load

```bash
./load.sh loader down 10
```

Notes:

- Loader queue drains gradually  
- Processor stays scaled up until Loader queue remains **below threshold** for the full **30s cooldown**  
- After cooldown, Processor returns to baseline (2 workers, queueDepth 100)

### Expected logs

```json
{"level":"info","crd":"autoscale.orkestra.io/v1alpha1, Kind=Processor","workers":2,"message":"autoscaler: worker pool resized"}
{"level":"info","crd":"autoscale.orkestra.io/v1alpha1, Kind=Processor","queueDepth":100,"message":"autoscaler: queue depth limit updated"}
{"level":"info","crd":"Processor","workers":2,"queueDepth":100,"message":"autoscaler: baseline restored"}
```

This is **cross‑runtime dependency scaling**:

- No restarts  
- No redeployments  
- No external metrics  
- No custom code  
- Just declarative autoscaling  

---

## Cross entry types and autoscaling

A katalog can declare multiple `cross:` entries for the same CRD — one for CR data, one for health, one for metrics. Only the entry with `protocol: metrics` (or a raw `endpoint`) is used to resolve autoscale conditions. The other types are for status templates and are ignored by the autoscaler.

```yaml
cross:
  - crd: loader
    source:
      host: "http://localhost:8080"
      protocol: cr    # CR fields and children — used in status templates
    as: loaderCRInfo

  - crd: loader
    source:
      host: "http://localhost:8080"
      protocol: health # operator health — used in status templates
    as: loaderHealth

  - crd: loader
    source:
      host: "http://localhost:8080"
      protocol: metrics # queue depth and worker metrics — used by autoscaler
      cacheFor: 30s
    as: loaderCRDInfo
```

The autoscale condition `field: cross.loader.metrics.queueDepth` resolves from the `protocol: metrics` entry. Without it, the autoscaler has no source and the condition is silently skipped.

---

## Cross-cluster

This is the same pattern when the sibling runs in a different cluster entirely. Deploy Orkestra there and expose its API via an ingress — then point `host` at that URL instead of localhost.

```yaml
cross:
  - crd: loader
    selector:
      name: production-loader
      namespace: loader-system
    source:
      host: "https://orkestra-prod.my-company-internal.com"
      cacheFor: 10s
    as: prodLoader
```

No code change. No special configuration. The host is just a URL.

---

## Cleanup

```bash
chmod +x cleanup.sh && ./cleanup.sh
```
