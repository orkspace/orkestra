# 01 — Without Autoscaler

This example runs two workers with a fixed queue and no autoscaling. It shows what happens when the queue fills up and what `behaviour:` does about it.

---

## Prerequisites

- Ork CLI  
- Kubernetes cluster (Kind works great)

Install Ork CLI:

```bash
curl get.orkestra.sh | bash
```

Create a Kind cluster to run this example:

```bash
ork create cluster
```

Start the Control Center:

```bash
ork control
# username:password → orkestra
```

Visit: **http://localhost:8081**

---

## Run the Example

### 1. Start the runtime

```bash
ork run
# Orkestra reads katalog.yaml, applies the CRD and cr.yaml, and starts the operator.
```

Watch the Control Center as resources are created:

- A **Deployment** and **Service** appear  
- **2 workers** stay mostly idle  
- Reconciliation happens every minute — `resync: 60s`

---

## The queue

By default, Orkestra queues are **unlimited** — they accept every event and nothing is dropped based on depth alone.

This example declares a limit and tells Orkestra what to do when it is reached:

```yaml
queue:
  maxDepth: 100
  behaviour:
    onLimit:
      drop: true
```

`maxDepth` sets the reference point. `behaviour.onLimit` is what tells Orkestra to drop when the queue hits that limit. Without a `behaviour:` block, `maxDepth` has no effect on whether items are dropped.

---

## Load the Ingestor

Now let's overload the operator with **150 Ingestor resources**.

```bash
./load.sh 150
```

Observe in the Control Center:

- Queue depth rises  
- Workers stay busy  
- Once the queue reaches **100**, new items are dropped per the `onLimit` declaration

### Expected logs

```json
{"level":"warn","key":"default/ingestor-123","gvk":"autoscale.orkestra.io/v1alpha1, Kind=Ingestor","limit":100,"depth":100,"message":"enqueue: queue depth limit reached — item dropped"}
{"level":"warn","key":"default/ingestor-124","gvk":"autoscale.orkestra.io/v1alpha1, Kind=Ingestor","limit":100,"depth":100,"message":"enqueue: queue depth limit reached — item dropped"}
```

The operator drops excess events once the queue is full. The CRs themselves are unchanged in etcd — the next resync re-enqueues them.

---

## What You Learned

- The queue is unlimited by default — drops only happen when `behaviour:` is declared  
- `maxDepth` sets the limit; `behaviour.onLimit.drop: true` is what causes the drop  
- Workers under load without autoscaling will saturate at whatever `maxDepth` is set to

In the **next example**, autoscaling handles the pressure instead — no drops, just more workers and a higher queue ceiling when conditions are met.

---

## Cleanup

```bash
chmod +x cleanup.sh && ./cleanup.sh
```
