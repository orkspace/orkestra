# watch fixture

Living fixture for `operatorBox.observe.watch`. A WatchProbe CR watches a shared
ConfigMap (`shared-config`) it does not own. When the ConfigMap changes,
the secondary informer resolves the primary CR key via `keyFrom.name` and
re-enqueues it — no ownerReference required.

Key points demonstrated:

- `operatorBox.observe.watch` sets up a dynamic informer for the watched resource.
- `keyFrom.name` and `keyFrom.namespace` maps the watched object's events to a fixed primary CR key (singleton pattern).
- Events during the initial cache sync are dropped — only real changes trigger re-enqueues.
- The primary CR's reconciler runs as normal; no Go code is needed to wire the watch.

```bash
ork validate    pkg/runtime/informer/fixture/watch/katalog.yaml
ork e2e      -f pkg/runtime/informer/fixture/watch/e2e.yaml
```
