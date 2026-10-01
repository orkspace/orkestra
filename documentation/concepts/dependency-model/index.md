# Dependency Model

When you have multiple CRDs that depend on each other, they need to start in the right order. Orkestra handles this automatically — no code required.

You declare the dependencies. Orkestra builds the graph, computes the startup order, waits for missing CRDs, and stops everything cleanly in reverse on shutdown.

---

## The problem

Imagine a web application that needs a database before it can run:

- The database CRD must be ready before the application CRD starts
- If the database CRD is missing, the application should wait
- When the database appears, the application should start automatically

Traditional operators don't handle this. You write code to check if dependencies exist, wait for them, and retry when they appear. Orkestra does it for you.

---

## Quick reference

| What | How Orkestra handles it |
|---|---|
| Declare dependencies | `dependsOn` — list, key-value map, or full map |
| Startup order | Automatic — dependencies first, via topological sort |
| Condition granularity | `started`, or `healthy` per dependency |
| Missing CRDs | Wait, retry in background, activate when they appear |
| Shutdown order | Reverse of startup |
| Circular dependencies | Detected at validate time and rejected at startup |

---

## Where to go next

- [Declaring Dependencies](01-declaring-dependencies.md) — three formats and condition values
- [Lifecycle](02-lifecycle.md) — graph building, missing CRDs, shutdown, and CLI visualization

---

## Try it

```bash
ork init --pack advanced
cd 13-dependencies/01-in-binary
```

Follow the README — it walks through `App` waiting for `Database` to be healthy, with live dependency-graph visualization.
