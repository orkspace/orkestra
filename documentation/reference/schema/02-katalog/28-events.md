# operatorBox.observe.events

`operatorBox.observe.events` declares Kubernetes Events that trigger reconciliation of the primary CR.

## Declaration

```yaml
spec:
  crds:
    app:
      operatorBox:
        observe:
          events:
            dbReady:
              reason: DatabaseReady
              type: Normal
              regarding:
                apiVersion: databases.example.com/v1
                kind: Database
                name: my-db

            dbFailed:
              reason: DatabaseFailed
              type: Warning
              regarding:
                apiVersion: databases.example.com/v1
                kind: Database
                name: my-db
```

`events` is a map. The map key is the Event declaration name and identifies the matching Event in the reconciliation context.

## `events[]` fields

| Field                 | Type              | Required | Description                                                                 |
| --------------------- | ----------------- | -------- | --------------------------------------------------------------------------- |
| `reason`              | string            | no       | Match the Event's `reason` field.                                           |
| `action`              | string            | no       | Match the Event's `action` field.                                           |
| `type`                | string            | no       | Match the Event's `type` field.                                             |
| `reportingController` | string            | no       | Match the Event's `reportingController` field.                              |
| `reportingInstance`   | string            | no       | Match the Event's `reportingInstance` field.                                |
| `namespace`           | string            | no       | Restrict matching Events to this namespace.                                 |
| `on`                  | `[]string`        | no       | Event lifecycle types: `create`, `update`, `delete`. Defaults to all three. |
| `regarding`           | `ManagedResource` | no       | Match the object referenced by the Event's `regarding` field.               |
| `related`             | `ManagedResource` | no       | Match the object referenced by the Event's `related` field.                 |
| `keyFrom`             | `WatchKeyFrom`    | no       | Define how the primary CR key is resolved.                                  |

At least one matching or routing field must be declared. An empty Event entry is rejected.

## Declaration name

Event declarations do not specify `apiVersion` or `kind`. The observed resource is always:

```text
events.k8s.io/v1/Event
```

The map key is the declaration name:

```yaml
events:
  dbReady:
    reason: DatabaseReady

  dbFailed:
    reason: DatabaseFailed
```

Declaration names must be **camelCase**. `ork validate` enforces this because the name is used by the resolver as the `.events.<name>` path.

The declaration name is independent of the Kubernetes Event's metadata name.

## Matching

Every non-empty field in an Event declaration is a matching constraint. Fields that are omitted are wildcards.

```yaml
events:
  databaseWarning:
    reason: DatabaseDegraded
    type: Warning
    reportingController: database.example.com/operator
```

An Event must match all specified fields.

`regarding` and `related` match the corresponding objects referenced by the Event. They are matching constraints and do not change how the Event is represented.

## ManagedResource

`regarding` and `related` use `ManagedResource`.

| Field        | Type   | Description                           |
| ------------ | ------ | ------------------------------------- |
| `apiVersion` | string | API version of the referenced object. |
| `kind`       | string | Kind of the referenced object.        |
| `name`       | string | Name of the referenced object.        |
| `namespace`  | string | Namespace of the referenced object.   |

Unspecified fields are wildcards.

```yaml
events:
  databaseEvents:
    regarding:
      kind: Database
```

## `on`

`on` specifies which Event lifecycle types trigger the declaration.

Supported values:

* `create`
* `update`
* `delete`

When omitted, all three are enabled:

```yaml
on: [create, update, delete]
```

Example:

```yaml
events:
  databaseReady:
    reason: DatabaseReady
    on: [create]
```

## `keyFrom`

`keyFrom` defines the primary CR key associated with a matching Event.

```yaml
events:
  databaseReady:
    reason: DatabaseReady
    keyFrom:
      label: app.kubernetes.io/cr-owner
```

Or:

```yaml
events:
  databaseReady:
    reason: DatabaseReady
    keyFrom:
      name: my-operator
      namespace: default
```

| Field       | Type   | Description                                               |
| ----------- | ------ | --------------------------------------------------------- |
| `label`     | string | Label key whose value is the primary CR key.              |
| `name`      | string | Name of the primary CR to enqueue.                        |
| `namespace` | string | Namespace of the primary CR. Only meaningful with `name`. |

### Validation

* Exactly one of `label` or `name` must be set when `keyFrom` is present.
* `namespace` cannot be combined with `label`.
* `namespace` is only meaningful with `name`.

## Multiple matching declarations

A Kubernetes Event can match multiple declarations.

```yaml
events:
  allDatabaseEvents:
    regarding:
      kind: Database

  databaseWarnings:
    type: Warning
    regarding:
      kind: Database
```

Each declaration retains its own name.

## Validation

`ork validate` enforces:

* At least one matching or routing field is present.
* `on` values are `create`, `update`, or `delete`.
* `keyFrom`, when present, has exactly one of `label` or `name`.
* `keyFrom.namespace` is rejected when `label` is used.
* `regarding` and `related` use valid `ManagedResource` fields.
* Event declaration names are camelCase.

Event observation always targets:

```text
events.k8s.io/v1/Event
```
