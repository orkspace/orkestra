---
title: "Trust and Failure Model"
date: 2026-05-29
weight: 2
---

Trust in a distributed system is not a feature you add — it is a property that either emerges from the design or does not. Orkestra's approach is to make trustworthy behavior the default at every layer, and to ensure those layers compound rather than exist independently. This is a description of how that works.

---

## The foundational guarantee

Every Orkestra reconciler is level-triggered. It does not replay a log of events — it reads current desired state and drives toward it. This is the same model Kubernetes uses for its own controllers, and it carries the same property: any operation that is interrupted half-way through leaves the cluster in a partial state, not a corrupted one. The next reconcile corrects it.

This guarantee is not contingent on clean shutdowns or graceful restarts. A process crash, a node failure, a SIGKILL — they all produce the same outcome. The next reconciler that runs will see current state and close the gap.

---

## Two processes, two trust domains

Orkestra deploys as two binaries: the Runtime and the Gateway. They have separate Kubernetes ServiceAccounts, separate ClusterRoles, and separate deployments. Neither process carries the permissions of the other.

The Runtime reconciles custom resources. It reads CRs, applies templates, manages the resources declared in `onCreate` and `onReconcile` blocks, and emits events. It has no permissions to manage webhook configurations or TLS certificates.

The Gateway serves admission webhooks — validation, mutation, and version conversion — and manages TLS automatically. It has no permissions to reconcile CRs or manage the resources your operator controls.

This split is structural, not policy. The RBAC that `ork generate bundle` produces is derived directly from what your Katalog declares. If your operator creates Deployments and Services, the Runtime gets exactly those permissions. The Gateway gets permissions for webhook configurations and certificate secrets, and nothing else. No wildcards. No cluster-wide write access unless the Katalog explicitly declares resources that require it.

The result is that a compromise of either process has a bounded blast radius. The Runtime cannot touch webhook infrastructure. The Gateway cannot touch your CRs or their child resources.

---

## The production binary is not the development binary

`ork` in development includes every command: `init`, `generate`, `validate`, `template`, `diff`, `upgrade`, `controlcenter`, and `run`. The production runtime is compiled with a build tag that removes everything except `run` and `version`. This is a compile-time exclusion — no configuration can re-enable it.

An attacker who reaches the container cannot use `ork generate` to modify cluster state, cannot use `ork init` to write arbitrary files, and cannot trigger any code path that exists only for local development. The runtime binary is a smaller, narrower artifact than the full CLI: fewer dependencies, fewer entry points, fewer things that can go wrong.

This is a narrow surface by construction, not by configuration.

---

## Isolation within the runtime

Each CRD in a Katalog runs inside its own **OperatorBox** — an isolated runtime cell that owns everything needed to reconcile that CRD independently. Its own informer. Its own event queue. Its own worker pool. Its own health state. Its own reconciler instance. Nothing is shared with any other CRD.

This is the same idea as containerization, applied inside a single binary. A CRD goes into an OperatorBox and comes out as an operator: self-contained, with its own resource scope and its own lifecycle. Thirteen operatorBoxes can run simultaneously in one process. They do not know each other exists.

The consequences are structural, not just operational. Queue pressure in one operatorBox does not affect reconcile latency in another. A panic in one reconciler — a nil pointer, an index out of range, any unrecovered Go panic — is caught, logged with the full stack trace, and requeued with backoff. The affected operatorBox retries. Every other operatorBox continues uninterrupted.

CRDs that need to observe each other's state do so through an explicit declaration — `cross:` in the operatorBox configuration — that names the source CRD, the CR selector, and the field to read. The read is in-memory, zero API calls for same-binary operatorBoxes. Startup sequencing works the same way: a `dependsOn` declaration tells Orkestra to hold this operatorBox's workers until another has reconciled successfully at least once.

Communication is always opt-in and always visible in the Katalog. What is not declared does not happen.

---

## Leader election and failover

Exactly one Orkestra instance actively reconciles at any time. Leadership is held via a Kubernetes Lease object — the same mechanism `kube-controller-manager` uses. While one instance leads, all other instances run their informers and maintain warm caches. They are not idle.

When the leader crashes or its node fails, the Lease expires after the configured lease duration. A follower acquires the lease immediately — it already has a warm cache and a queue populated with pending events. Reconciliation resumes within seconds, not minutes.

During that window, CRs are not modified or deleted. They wait in etcd, unchanged, until the new leader processes them.

---

## How the layers compound

None of these properties work in isolation — they build on each other.

The level-triggered reconciler assumes it will be interrupted. The panic recovery assumes individual reconciles will fail. The isolated worker pools assume one CRD's failure will happen. The isolated workqueue assumes one CRD's event rate will saturate its capacity. The leader election assumes the entire process will crash. The production binary assumes someone might try to misuse whatever surface is exposed.

Each layer is designed for the one below it to fail, and to remain correct when it does. The result is a system where trustworthy behavior does not depend on everything going right — it depends on the guarantees holding even when things go wrong.

---

## Admission and the Gateway

The Gateway adds a second enforcement point on top of the runtime guarantees. Every CR creation and update passes through the Gateway's admission webhooks before it reaches etcd. Validation rules catch schema violations at admission time. Mutation rules apply defaults synchronously. Conversion webhooks handle schema evolution across CRD versions.

If the Gateway is unavailable during admission, the configured `failurePolicy` determines behavior. The default is `Ignore` — CRs are admitted and the Runtime validates them at reconcile time. This keeps the system non-blocking during a brief outage. If synchronous enforcement is required, `failurePolicy: Fail` ensures no CR reaches etcd without Gateway approval.

Deletion protection is enforced by the Gateway. A CR with the protection label cannot be deleted without either removing the label first or using the explicit override mechanism. In strict mode, deletion attempts are rejected outright at admission — they never reach the Runtime.

These admission-layer guarantees exist independently of the reconcile loop. They are enforced before the Runtime sees the resource.

---

## What this looks like in practice

The Katalog is the source of truth for what permissions the runtime needs. `ork generate bundle` reads the Katalog and produces a YAML bundle — Namespace, ServiceAccounts, ClusterRoles, ClusterRoleBindings — containing exactly those permissions and nothing more. The bundle diffs cleanly in GitOps workflows. You see what is being added or removed before it reaches the cluster.

`ork validate` reads the Katalog and shows you the security posture of each CRD before deployment: which permissions will be requested, which admission rules are active, and whether any configuration conflicts with the security model.

The flow from Katalog to running operator is auditable at every step. No step requires cluster-admin access. No step produces broader permissions than the Katalog declares.

---

For a complete description of the security mechanisms — deletion protection, namespace restrictions, admission webhook configuration, RBAC generation, and binary provenance — see the [Security](/docs/security/) documentation.
