# operatorBox.observe.watch

`operatorBox.observe.watch` declares secondary Kubernetes resources to observe.

## Declaration

```yaml
spec:
  crds:
    app:
      operatorBox:
        observe:
          watch:
            - apiVersion: apps/v1
              kind: Deployment
              namespace: default
              on: [update]
              keyFrom:
                label: app.kubernetes.io/cr-owner
````

## Fields

| Field        | Type           | Required | Description                                                              |
| ------------ | -------------- | -------: | ------------------------------------------------------------------------ |
| `apiVersion` | string         |      yes | API version of the watched resource.                                     |
| `kind`       | string         |      yes | Kind of the watched resource.                                            |
| `namespace`  | string         |       no | Namespace of the watched resource.                                       |
| `name`       | string         |       no | Name of the watched resource.                                            |
| `on`         | `[]string`     |       no | Observation events: `create`, `update`, `delete`. Defaults to all three. |
| `keyFrom`    | `WatchKeyFrom` |       no | Defines how the primary CR key is resolved.                              |
| `index`      | `[]WatchIndex` |       no | Cache indexes for fields used by lookups.                                |

## keyFrom

| Field       | Type   | Description                                    |
| ----------- | ------ | ---------------------------------------------- |
| `label`     | string | Label whose value identifies the primary CR.   |
| `name`      | string | Name of the primary CR to enqueue.             |
| `namespace` | string | Namespace of the primary CR. Used with `name`. |

### Validation

* `apiVersion` is required.
* `kind` is required.
* `on` values must be `create`, `update`, or `delete`.
* `keyFrom` must specify exactly one of `label` or `name`.
* `keyFrom.namespace` cannot be used with `keyFrom.label`.
* Duplicate watch identities are rejected.

## Include

Watch declarations can be loaded through `operatorBox.observe.include`.

```yaml
operatorBox:
  observe:
    include: observe.yaml
```

The included file may contain `watch:` and `events:` declarations.

## Index

`index` declares fields to index in the watcher's informer cache.

```yaml
index:
  - name: metadata.labels.app
    field: metadata.labels.app
```

## Defaults

If `on` is omitted, the watch reacts to:

```yaml
on: [create, update, delete]
```