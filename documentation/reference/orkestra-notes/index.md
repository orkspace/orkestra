# Orkestra Notes

Notes are pure transformation functions available in every `{{ }}` expression across a Katalog — status fields, `when:` conditions, resource names, `onCreate`, `onReconcile`, mutation rules, conversion paths. They are the vocabulary of declarative operator behavior.

**Contract:** pure (same input → same output), safe (nil/empty input never panics), stateless (no I/O, no side effects). Use katalog `external` block or [hooks](../../concepts/typed-operators/01-hooks.md) for anything requiring external calls.

---

## Why notes matter

Without notes, a Katalog can only copy field values as-is. With notes, it can reason about them — fall back to a default when a field is absent, gate a deployment on a job completing, compute a name from multiple fields. Notes are what make declarative operators expressive enough to replace hand-written reconcilers for most use cases.

---

## Two notes to know first

### `default` — supply a fallback value

```yaml
status:
  fields:
    - path: replicas
      value: "{{ default .spec.replicas 1 }}"
```

If `spec.replicas` is absent or zero, the status field shows `1`. If it is set, it passes through unchanged.

### `jobSucceeded` — gate on a completed Job

```yaml
onReconcile:
  jobs:
    - name: "{{ .metadata.name }}-migration"
      image: "{{ .spec.image }}"
      command: "{{ .spec.command }}"

  deployments:
    - name: "{{ .metadata.name }}-api"
      image: "{{ .spec.image }}"
      when:
        - field: "{{ jobSucceeded .children.job }}"
          equals: "true"
```

The api deployment is not created or updated until `status.succeeded > 0` on the migration job.

---

## ork notes — explore from your terminal

`ork notes` is the fastest way to discover and learn about every note available in your version of Orkestra.

**List all notes:**
```bash
ork notes
```

**Search by keyword:**
```bash
ork notes search job
```

**Show full detail and an example for one note:**
```bash
ork notes show jobSucceeded
```

---

## All notes by domain

| Domain | Notes |
|--------|-------|
| Cron | `cronToMap` `cronFromMap` `cronFromAny` `cronNormalize` `cronDescribe` `cronValid` `cronField` `cronMinute` `cronHour` `cronDom` `cronMonth` `cronDow` `cronExpr` |
| Semver | `semverMajor` `semverMinor` `semverPatch` `semverValid` `semverCompare` `semverBump` `semverConstraint` |
| String | `toLower` `toUpper` `trimSpace` `trim` `trimPrefix` `trimSuffix` `hasPrefix` `hasSuffix` `contains` `replace` `split` `join` `repeat` `camelToKebab` `truncate` |
| Math | `add` `sub` `mul` `div` `mod` `min` `max` `clamp` `abs` |
| Conditional | `ternary` `boolTernary` `boolDefault` `coalesce` `default` `empty` `notEmpty` |
| Type | `toInt` `toFloat` `toBool` `toString` `typeOf` `typeMap` `typeList` `typeString` `typeNumber` `typeBool` `typeNull` `len` `Empty` `isScalar` `isComposite` |
| Data | `toBase64` `fromBase64` `toJSON` `sha256sum` `slugify` `truncateName` |
| Random | `randomAlphanumeric` `randomHex` `randomBase64` |
| Time | `timeAgo` `timeSince` `timeFormat` `isExpired` `durationSeconds` `durationAdd` `durationValid` |
| Network | `cidrContains` `ipValid` `ipIsPrivate` |
| Quantities | `parseQuantity` `formatQuantity` `sumQuantity` |
| List / Map | `listHas` `listGet` `listLen` `mapGet` `mapKeys` `mapValues` `mapPick` `mapOmit` |
| Safe access | `getOr` `getStringOr` `getIntOr` `getBoolOr` |
| Type coercion | `asList` `asMap` `asString` |
| Kubernetes | `get` `meta` `name` `namespace` `labels` `annotations` `spec` `status` `phase` `resourceName` `resourceNamespace` `resourceUID` `resourceVersion` `creationTimestamp` `ownerKind` `ownerName` `hasCondition` `conditionReason` `conditionMessage` `resourceExists` `isTerminating` `generation` `observedGeneration` `isSynced` |
| Replicas | `replicasReady` `desiredReplicas` `readyReplicas` `availableReplicas` `updatedReplicas` |
| Service | `serviceClusterIP` `serviceNodePort` `serviceLoadBalancerIP` `serviceLoadBalancerHost` `endpointsReady` |
| Container | `containerImage` `containerEnv` `containerPort` `containerPortByName` `containerPorts` `containerCount` |
| Job | `jobSucceeded` `jobFailed` `jobActive` |

Each domain has a dedicated reference page linked in the sidebar.

---

## Adding a note

1. Add the function to `pkg/note/<domain>.go`
2. Register it in that file's `xxxNotes()` FuncMap function
3. If it is a new file, register it in `buildNotes()` in `pkg/note/note.go`
4. Document it in the corresponding domain file and update the table above
5. Run `make build`

**Contract:** handle nil/empty input with a safe zero value, not a panic. Return `(value, error)` for functions that can meaningfully fail; return just `value` for infallible ones.
