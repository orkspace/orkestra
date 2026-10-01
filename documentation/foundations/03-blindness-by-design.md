# Blindness by Design

When two components know too much about each other, changing one breaks the other. In Kubernetes operator development this happens constantly — resource templates written for one specific operator, operator behavior tightly coupled to the infrastructure that runs it, a runtime that assumes it knows where its configuration came from. The coupling is invisible until the moment you try to upgrade something, and then it is everywhere.

Orkestra separates these concerns into distinct layers: reusable resource templates, operator behavior declarations, and the infrastructure — runtime and gateway — that runs them. Each layer is intentionally opaque to the layers around it.

A Motif has no knowledge of the Katalog that imports it. A Katalog has no knowledge of the Komposer that composes it. The Runtime has no knowledge of the registry that published the pattern. None of this is an oversight. Each layer is blind to the next by design — because that blindness is what makes the composition model work at scale, across teams, and across registries you do not control.

---

## The only contract is the Katalog

Orkestra has one universal contract: the **Katalog**. It is what you declare. It is what the Runtime runs. Everything else — Motifs, Komposers, registries — is infrastructure for producing or distributing Katalogs. The Katalog itself is the boundary where your intent meets the system.

This contract has exactly two participants: **your declaration** and **Kubernetes**.

Orkestra does not need to know where your Katalog came from. It does not need to know what Motifs it was built from, which Komposer imported it, or which registry published it. It reads the Katalog. It runs it. That is all.

```text
Your declaration (Katalog)  +  Kubernetes  =  Running operator
```

Nothing else is required. Nothing else is assumed.

---

## Each layer is blind to what the next does

**Motifs** are resource templates. A Motif declares inputs and the resources it expands into. It has no knowledge of the Katalog that will import it, which CRD it will be bound to, which CR fields will be passed as inputs, or whether anyone imports it at all. It simply defines a reusable shape.

**Katalogs** import Motifs and declare operators. A Katalog knows its own CRDs, its own reconcile behavior, its own admission rules. It has no knowledge of the Komposer that composes it, the platform that deploys it, or the cluster it runs in. It simply declares intent.

**Komposers** compose Katalogs. A Komposer knows which Katalogs to import and what overrides to apply. It has no knowledge of how any individual Katalog works internally, what Motifs it uses, or what child resources it creates. It simply combines.

**The Runtime** knows the Katalog and Kubernetes. It knows nothing about the registry, the Komposer, the Motifs, or any tooling that produced the Katalog. It reads the resolved declaration and reconciles against the cluster API. That is its entire job.

This is not incidental. It follows directly from [Do One Thing Well](04-do-one-thing-well.md) — a layer that does only one thing needs to know only what that one thing requires. Blindness is the consequence of focus.

---

## You can stop at any layer

The layering is additive, not mandatory. You do not need to use all of it.

**Motifs only** — write a Motif, validate it, publish it. Other teams import it. You are done. The Motif does not know or care whether anyone uses it.

**Katalogs only** — write a Katalog that imports local Motifs or declares resources directly. Run it with `ork run`. Publish it to the registry. Never write a Komposer. A Katalog is a complete, deployable operator on its own.

**Komposers** — compose multiple Katalogs into a single Runtime deployment. This is an optional coordination layer. It does not change what any individual Katalog does; it only determines which Katalogs run together.

You choose how far to go. The system does not pull you further than your problem requires.

---

## The Runtime and Gateway are blind to each other

The blindness between artifact layers — Motif, Katalog, Komposer — is easy to see. Less obvious is that it applies between the production components themselves.

The Runtime reconciles CRs. On every cycle, before creating any child resource, it re-checks the admission rules declared in the Katalog — validation rules, deletion protection, mutation. It does not know whether a Gateway is running. It does not know whether that Gateway intercepted the CR at admission time. It enforces the rules because they are its rules to enforce, unconditionally.

The Gateway serves admission webhooks. When a CR arrives at the API server, the Gateway validates it, mutates it if needed, and either admits or rejects it — before the Runtime ever sees it. The Gateway does not know whether the Runtime will enforce the same rules on every subsequent reconcile. It does not need to.

Both enforce. Neither defers to the other. Neither knows whether the other is present.

This is what makes the two-layer protection real. If the Gateway is absent — not deployed, or temporarily unavailable — the Runtime still enforces at reconcile time. If the Runtime restarts mid-cycle, the Gateway still caught the admission. A CR that slips past one layer runs into the other. The protection does not depend on coordination because there is no coordination to break.

The blindness between them is not a gap. It is what guarantees that both layers enforce independently.

---

## Blindness scales across trust boundaries

The same property that makes composition work within a team makes it work across teams and across registries.

A platform engineer imports a Postgres Katalog from the official Orkestra registry and a WebApp Katalog from an internal registry. The Komposer that combines them knows only that both exist and what overrides to apply. It does not know how Postgres is implemented, what Motif it uses, or what version of the Postgres image is set as default.

The Postgres Katalog does not know it is being composed with WebApp. It does not know it came from a public registry while WebApp came from an internal one. It behaves identically in both cases because it is blind to both.

This is what makes cross-registry, cross-team composition safe. There is no hidden dependency on what any other team is doing. The contract is the Katalog. The contract is public. Everything else is internal.

---

## The orchestra analogy

A violinist in an orchestra knows how to play the violin. That is their job. They do not need to know how the cellos work, what the percussion section is doing, or how the conductor chose the tempo. They play their part.

Together — without any one of them knowing what the others are doing internally — they produce a symphony. The harmony emerges from composition, not from coordination.

Orkestra is built on this principle. Each layer does its job. The Runtime reconciles the Katalog. The Katalog imports the Motif. The Komposer combines the Katalogs. None of them need to understand each other's internals. The composition produces the platform.

The blindness is not a constraint. It is what makes the symphony possible.
