# Do One Thing Well

The more responsibilities something accumulates, the harder it is to reason about, the more it knows about things it shouldn't, and the more it breaks when something adjacent changes. This is true for software in general. In operator infrastructure, where components compose across teams and registries, it becomes a stability problem.

Each piece of Orkestra has exactly one responsibility. Not "one primary responsibility with some secondary ones." One.

This is the Unix philosophy applied to operator infrastructure. A tool that does one thing can be understood completely. It can be composed freely. It can be replaced, upgraded, or extended without disturbing anything that depends on it.

---

## From packages to CLI

The principle is not a design guideline applied at the architecture level and then forgotten inside the codebase. It runs from the Go packages up through the CLI to the artifacts users write.

`main.go` is 11 lines of actual code. It loads configuration, initializes a context, and calls `cli.Execute`. That is all it does — because it is the entry point, not the program. The program lives in the packages.

Each package has one job: `pkg/konfig` loads and validates configuration, `pkg/logger` provides structured logging, `pkg/merger` resolves and merges Katalog sources, `pkg/registry/simulate` runs the in-memory reconcile loop, `pkg/registry` handles OCI push and pull. None of them know about each other unless the dependency is explicit and necessary.

The CLI commands `(cmd/cli)` are thin wires between user input and package logic. A command parses flags, calls a package function, and formats the result. It does not contain business logic.

This is why the `runtime` build tag works cleanly. The production binary includes only `cmd/cli/run.go` and the packages it depends on. Every other command and its transitive dependencies are absent — not stubbed, not gated, absent. The surface is minimal because the code was written to be minimal.

---

## Each artifact has one role

**Motif** — a reusable resource template. A Motif declares named inputs and the Kubernetes resources it expands into when those inputs are bound. It does not reconcile. It does not run. It does not know who imports it.

**Katalog** — an operator declaration. A Katalog declares one or more CRDs and how to reconcile them: what resources to create, what rules to enforce, what status to propagate. It is a complete operator. It does not know how it was built or who composes it.

**Komposer** — a composition. A Komposer names the Katalogs to run together and any overrides to apply. It does not declare CRDs. It does not contain reconcile logic. It composes.

**Simulate** — a behavioral assertion set. A `simulate.yaml` declares what the reconciler should create and in which cycle. It does not deploy. It does not test a live cluster. It asserts.

**E2E** — a cluster test. An `e2e.yaml` applies CRs to a real cluster and asserts that the right resources appear. It does not simulate. It does not gate publication directly. It tests.

Each of these can be used without the others. A Katalog can be written without Motifs. An operator can be deployed without a Komposer. Simulate and E2E can each be run independently. The layering is additive because each layer is complete on its own.

---

## Orkestra itself is three things — each doing one

The principle applies to the artifacts you write. It also applies to Orkestra as a system.

Orkestra ships as three separate binaries, each with a single job:

| Component | One job |
|-----------|---------|
| **Runtime** | Reconcile CRs against Kubernetes |
| **Gateway** | Handle admission — validation, mutation, deletion protection, version conversion |
| **Control Center** | Observe — surface health, events, and drift across the cluster |

None of the three knows how to do the other's job. The Control Center cannot reconcile. The Runtime cannot serve admission webhooks. The Gateway cannot render the dashboard. They are not feature flags on a single binary. They are separate processes, separate ServiceAccounts, separate ClusterRoles, and separate containers — each with a surface area limited to exactly what its job requires.

This means each component can fail, be replaced, or be omitted independently. Deploy without a Gateway when you do not need admission webhooks. Run without a Control Center in an environment where observability is handled elsewhere. The system is additive because each piece is complete on its own.

---

## The Runtime does one thing

The Runtime knows the Katalog and Kubernetes. It reads the resolved Katalog at startup, and it reconciles CRs against the Kubernetes API for as long as it runs.

It does not know where the Katalog came from. It does not know whether a Komposer produced it. It does not know which registry the Motifs came from. By the time the Runtime sees the Katalog, all of that context has been resolved away. The Runtime sees a complete declaration, and it runs it.

This is why `ork run` locally and the production Helm deployment behave identically. They are the same binary running the same code on the same input. The only variable is the Katalog.

---

## The Gateway does one thing

The Gateway handles admission — validation, mutation, deletion protection, namespace protection, and version conversion. It registers webhook configurations with the API server, serves the HTTPS endpoints those webhooks call, and manages TLS automatically.

It does not reconcile CRs. It does not read the reconcile templates. It does not know what child resources your operator creates. It knows the admission rules you declared and the Kubernetes API for registering webhooks.

A compromise of the Gateway cannot reach the reconciler. A compromise of the Runtime cannot touch the webhook infrastructure. The separation is enforced by separate Kubernetes ServiceAccounts, separate ClusterRoles, and separate processes — each doing exactly one thing.

---

## The Control Center does one thing

The Control Center observes. It surfaces health state, reconcile events, drift, and status across every CRD the Runtime manages — in one dashboard, across namespaces.

It does not reconcile. It does not validate. It does not intercept API calls. It reads from the same Kubernetes API the Runtime writes to, and it presents what it finds. That is its entire job.

Because the Control Center has no write permissions to CRs or cluster resources, it cannot affect the behavior of the system it watches. You can port-forward it, expose it, or shut it down without changing anything about how the Runtime or Gateway behave. Observation and operation are separated by design — the observer cannot become an actor.

---

## Why this compounds with Blindness

[Blindness by Design](03-blindness-by-design.md) is the consequence of Do One Thing Well.

A layer that does only one thing needs to know only what that one thing requires. The Runtime needs the Katalog and Kubernetes. It does not need the registry, the Komposer, or the Motifs — so it does not know about them. The Motif needs to declare inputs and resources. It does not need to know the Katalog, the Komposer, or the Runtime — so it does not know about them.

The blindness is not imposed from outside. It falls out naturally from the constraint that each piece has one job. Give something one job and it will only need to see what that job requires. Give it multiple jobs and it starts accumulating dependencies.

Orkestra stays composable because every piece stays focused. A platform composed of a hundred Katalogs from a dozen registries works because no piece needed to know about any other piece to do its job correctly.
