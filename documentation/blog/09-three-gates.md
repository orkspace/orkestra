# Three gates

Orkestra started with two places where you could decide whether something should happen: at admission (before the CR hits etcd) and inside the reconciler (once it was there). That was the model, and it worked. Then the runtime and gateway split into separate processes. Then queue behaviour arrived. Then a new query interface. By the end, there were three gating points, each with a distinct job and a distinct reason for existing.

This is how that happened.

---

## Two gates

The original design was simple. The gateway handled admission — validation, mutation, whether a value was unique. The runtime handled reconciliation — creating resources, checking conditions, deciding what to skip. Two components, two moments of decision, clear ownership.

The interesting work happened inside the reconciler. Resource-level `when:` conditions let you skip individual resources based on current state. Hooks gave you Go code for anything that needed real logic. The reconciler ran, or it didn't, based on what it found.

---

## The split

When the runtime and gateway became separate processes, Kubernetes became the communication channel. This was the right call — Kubernetes is already there, already consistent, already the source of truth for object state. Cross-component communication through Kubernetes objects works well for state that belongs in Kubernetes.

But not all state belongs in Kubernetes.

The runtime knows things that Kubernetes does not: how many items are currently in the queue, whether the operator has been reconciling cleanly or has been failing for the past five cycles, what the informer cache contains at this instant. This is runtime-domain state — it exists because the runtime exists, and it disappears when the runtime does.

When the gateway needed to make admission decisions using this state — is the queue healthy enough to accept this CR? is this value unique among what the informer currently sees? — Kubernetes was not the answer.

---

## Health and metrics as annotations

The first approach was to write the runtime's health and metrics onto the CR as annotations. The runtime stamped them after each reconcile; the gateway could read them from the object during validation.

This worked for one case: preReconcile gates. Once annotations existed, `reconcileGate.when:` conditions could read the runtime's health and metrics from the object's annotations and gate the reconcile on that value. Gate conditions on live operational state, without any new infrastructure. That part worked well, and it is still how preReconcile reads runtime-native state today.

But annotations were not the answer for admission. Admission happens before the CR is written to etcd — the annotations don't exist yet for new CRs, and even for updates, they carry the state from the last reconcile, not the current moment. A uniqueness check against annotations would be checking yesterday's list.

---

## A query interface

What the gateway needed was a way to ask the runtime a question and get a live answer. Not a Kubernetes object, not an annotation — a direct call, at admission time, to the runtime's own API.

The `domain.RuntimeQuery` interface is that mechanism. It defines three questions the gateway can ask:

- Is this value unique right now among existing CRs? (`IsUnique`)
- What is the operator's current health? (`ForHealth`)
- What are the current queue metrics? (`ForMetrics`)

`IsUnique` reads from the runtime's informer cache — best-effort, not authoritative. Two concurrent admissions can both pass; the reconciler's live check handles the conflict if that happens. `ForHealth` and `ForMetrics` return live operational stats from the runtime's own endpoints.

This is not ONCOP. ONCOP is operators observing other operators' CR data inside the reconcile pipeline. The runtime query is the gateway asking the runtime about its own internal state at admission time — state that has no Kubernetes encoding because it was never meant to be stored in Kubernetes.

---

## The third gate

Queue behaviour was a separate thread that arrived around the same time.

The queue already had `maxDepth`. When it was hit, items were dropped. That was useful — it was intentional back-pressure — but it was unconditional. A drop at 3 AM during a maintenance window is different from a drop at noon during peak traffic. The queue didn't know that.

`behaviour:` made the drop decision conditional. `onLimit:` fires when the queue is at capacity. `onThreshold:` fires earlier, at a declared percentage. Both accept `when:` and `or:` conditions — the same conditions available in gate expressions anywhere else. Outside business hours, drop. During business hours, let it through.

The interesting part was where to evaluate the conditions. The workqueue is arithmetic — it knows depth and thresholds, but it has no resolver context. Conditions that reference time functions, sentinel values, or external calls need the full preReconcile evaluator. So queue behaviour evaluation split into two tiers: the workqueue checks the arithmetic and sets a flag; the informer picks it up and evaluates the conditions with the full context. The item is not dropped until both agree.

This made the queue boundary a gating point in its own right — not just a buffer, but an active part of the decision about whether to proceed.

---

## Three gates today

The result is three distinct points where Orkestra decides whether to act on a CR:

**Admission** — before the CR reaches etcd. The gateway validates, mutates, and queries the runtime for live state. A denied CR never enters the system.

**preReconcile** — after the informer delivers a watch event, before the item enters the reconcile pipeline. `enqueueGate` acts before the queue; `reconcileGate` acts after dequeue. Queue behaviour fires here too. Each sub-gate has access to different information and produces a different health outcome.

**Reconcile** — inside the reconcile loop, for individual resources. The reconciler has full cross-CRD observation, external call results, and note values. Resource-level conditions decide whether specific resources are created or skipped.

Each tier handles what it is positioned to handle. Admission has write-time authority and live runtime access. preReconcile has event-time information — what changed, what the current state is, what the queue looks like. The reconcile loop has the full resolver context and acts on individual resources rather than the reconcile cycle as a whole.

The split that created the gateway was what made the third gate necessary. And the third gate was what made the query interface necessary. Each piece followed from the one before it.
