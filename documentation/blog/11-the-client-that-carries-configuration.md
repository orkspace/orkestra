# The Client That Carries Configuration

Every resource in Orkestra applies to Kubernetes via server-side apply.
That patch call has one flag that matters more than the others: `Force`.

When `Force` is true, the field manager asserts ownership over every
field it writes. Conflicts are resolved in its favour. This is what you
want for an operator runtime — it should own the resources it manages,
and if something else has been touching them, the operator should win.

`Force: true` had been hardcoded everywhere. All twenty-plus resources,
the same value. It was technically correct for the default case and
never questioned.

Then the question arrived from two directions at once.

---

## Two callers, one missing knob

The first direction: declarative operators. The `operatorBox` already
lets operator authors control almost everything about the resources
they declare — labels, annotations, image, replicas, resource limits.
But they couldn't say "this resource, don't force-own it." The field
conflict behaviour was invisible to them, hardcoded underneath.

The second direction: library users. `pkg/resources` is not just the
generic reconciler's implementation detail. It is usable as a Go
library — someone building a constructor can use Orkestra's resource
types instead of writing their own deployment and service structs. But
those callers inherit `Force: true` without asking for it. If they want
standard Kubernetes apply semantics, they have to reimplement the patch
call from scratch.

Neither should be true. The resolution hierarchy was obvious once the
problem was named:

```text
resource-level setting     (most specific — declared per resource)
      │
      │ if not set
      ▼
CRD-level setting          (fallback — declared once for all resources)
      │
      │ if not set
      ▼
system default: true       (the runtime owns what it manages)
```

The challenge was how to get the CRD-level setting down to the
resources package without creating a dependency that didn't belong.

---

## The client was already doing this

`pkg/resources` does not know about CRD entries. It should not know
about CRD entries. It knows about resource specs and a kube client
and how to apply them. That separation is deliberate.

But the kube client was already carrying things downward. Informers
travel this way. Event recorders travel this way. Args — the user-facing
configuration from `hooks.args` and `constructor.args` — already travel
this way. The `kubeclient.Interface` was already a downward channel for
context that reconcilers needed but that the resources package shouldn't
have to source independently.

The CRD-level `forceConflict` fit the same pattern.

Two methods on the interface:

```go
WithForceConflict(forceConflict *bool) Interface
ForceConflict() *bool
```

At construction time, the runtime sets the CRD-level value:

```go
kube.WithForceConflict(crd.ResolveForceConflict())
```

Inside `pkg/resources/shared`, the resolution is ten lines that don't
know where the CRD-level setting came from:

```go
func ResolveForceConflict(kube kubeclient.Interface, resourceForceConflict *bool) *bool {
    if resourceForceConflict != nil {
        return resourceForceConflict
    }
    if kube != nil {
        if fc := kube.ForceConflict(); fc != nil {
            return fc
        }
    }
    defaultForceConflict := true
    return &defaultForceConflict
}
```

The resource asks: was I configured? No — ask the client. Was the
client configured? No — use the system default.

Three layers of resolution in one function. Each layer ignorant of the
layers above it. Correct about its own concern.

---

## What this opens

Every resource now calls this function at apply time. The result flows
through to the Kubernetes patch options. Nothing else changes.

The generic reconciler's YAML authors can now write:

```yaml
deployments:
  - name: "{{ .metadata.name }}"
    forceConflict: false
```

And get standard Kubernetes apply semantics for that specific
deployment, while other resources in the same operatorBox continue
to use the runtime's default ownership behaviour.

A constructor author who calls `pkg/resources` directly passes their
kube client — already configured with `WithForceConflict` at
construction time — and gets the right behaviour without writing a
single patch call themselves. The library they are using becomes
configurable through the same mechanism as everything else in Orkestra.

This is the library goal stated precisely: do it once in the resources
package, use it correctly from the generic reconciler, from hooks, from
constructors, from external Go callers. The kube client is the channel
that connects all of them without any of them knowing about the others.

---

## The pattern

`forceConflict` is the second non-infrastructure configuration to travel
through `kubeclient.Interface`. Args was the first.

Both follow the same shape: the configuration originates in the
user-facing YAML, gets resolved at construction time by the runtime,
gets attached to the kube client with a `With*` method, and becomes
available to any caller that receives the client — without that caller
needing to know where the configuration came from.

When the next piece of CRD-level configuration needs to travel this
path, the pattern is already there.