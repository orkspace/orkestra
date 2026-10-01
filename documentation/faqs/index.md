# Frequently Asked Questions

## [Concepts](./01-concepts.md)

Core concepts — what Orkestra is, how it works, and how it compares.

- What is Orkestra?
- Is Orkestra an operator?
- Does Orkestra install my CRDs?
- Are Katalog, Komposer, Motif, E2E, and Simulate Kubernetes CRDs?
- Do I need to write Go code?
- How does Orkestra differ from Helm or Kustomize?
- What is a Katalog? What is a Komposer?
- What is a Motif?
- What is the note expression language?
- What is the OrkestraRegistry?
- What is the super-operator model?
- Does Orkestra support multi-version CRDs?
- What is the OPPRE execution model?

---

## [Running](./02-running.md)

Setup, configuration, operations, and RBAC.

- Can Orkestra manage multiple CRDs?
- How do I start Orkestra?
- What does `ork validate` do?
- Does Orkestra require cert-manager?
- What environment variables does Orkestra read?
- What RBAC permissions does Orkestra need?
- What does `ork generate bundle` do, and when do I re-run it?
- How do I debug a CRD in production?
- What is the Control Center?
- Is Orkestra safe for production?
- Does the deletion protection webhook protect Orkestra itself?
- What happens when Orkestra restarts?

---

## [Usage](./03-usage.md)

Common usage patterns — built-in kinds, validation, mutation, conditions.

- Can Orkestra manage built-in Kubernetes resources?
- What is the difference between validation and mutation?
- Does `ENABLE_ADMISSION_WEBHOOK=true` block the API server if Orkestra is down?
- How do I use `when:` conditions?
- How does `dependsOn` ordering work?
- What does `forEach` do?
- What is the difference between `normalize:` and `conversion.paths:`?
- When do I need Go hooks?
- When do I need a constructor?
- Can I mix declarative, hooks, and constructor operators in one binary?

---

## [Ecosystem](./04-ecosystem.md)

Comparisons and the path forward.

- How does Orkestra compare to ArgoCD?
- How does Orkestra compare to Kubebuilder and Operator SDK?
- How does Orkestra compare to kro?
- Can Orkestra manage third-party CRDs?
- What is the path to Kubernetes core?

---

## [Testing](./06-testing.md)

Simulate, E2E, and the testing tools.

- How do I test an Orkestra operator?
- What is the difference between `ork simulate` and `ork e2e`?
- How do I generate a `simulate.yaml`?
- What does `--debug-ops` do?
- What does `skipExternal: true` do?
- What is `ork simulate --dev-server`?
- Can I run simulate for multiple operators at once?
- Does simulate run the real reconciler?

---

## [Registry](./07-registry.md)

Publishing patterns and quality gates.

- How do I publish a Katalog to the registry?
- How do I publish a Motif?
- How do simulate and e2e gate publication?
- Can I use a private registry?

---

## Why Katalog and Komposer are not CRDs

See [Why Not CRDs](./05-why-not-crds.md) for the full reasoning behind keeping Katalog
and Komposer as plain YAML files rather than Kubernetes CRDs.

---

## Where to go next

- **[Getting Started](../getting-started/index.md)** — your first operator in minutes
- **[Security](../security/index.md)** — admission control, RBAC, namespace protection
- **[Deploying](../deploying.md)** — running Orkestra in a real cluster
- **[Roadmap](../roadmap.md)** — what is shipped and what is coming
