# Note Library — Developer Documentation

Notes are the template function library available in every Orkestra template expression — `status.fields`, `normalize.spec`, `onCreate`, `onReconcile`, mutation rules, and validation rules.

A note is a **pure, named transformation function**. Notes receive values and return transformed values. They cannot perform I/O, call external APIs, or produce side effects.

## This directory is the source of truth

The files here drive two generated outputs — run `make generate-notes` to refresh both:

| Output | What it does |
|--------|--------------|
| `pkg/note/catalog_generated.go` | Note registry used by the Orkestra runtime and the `ork notes` CLI command |
| `documentation/reference/orkestra-notes/<domain>.md` | User-facing reference pages on the documentation site |

Add documentation here first. The generator handles the rest. **Do not hand-edit the generated outputs.**

Files containing an `## In Development` heading are excluded from both outputs until the feature is ready.

## How notes are used

In any Katalog template expression:

```yaml
status:
  fields:
    - path: phase
      value: "{{ boolTernary .spec.suspend \"Suspended\" \"Active\" }}"

normalize:
  spec:
    schedule: "{{ if typeMap .spec.schedule }}{{ cronFromMap .spec.schedule }}{{ else }}{{ cronNormalize .spec.schedule }}{{ end }}"

onCreate:
  secrets:
    - name: "{{ .metadata.name }}-creds"
      once: true
      data:
        password: "{{ randomAlphanumeric 32 }}"
```

Notes work in both `value:` and `field:` positions — use template syntax for `field:`:

```yaml
when:
  - field: "{{ allReplicasReady.children.deployment }}"
    equals: "true"
  - field: "{{ resourceExists .children.secret }}"
    equals: "true"
```

## Documents

| File | Notes covered |
|------|---------------|
| [01-strings.md](01-strings.md) | `toLower` `toUpper` `trimSpace` `trim` `trimPrefix` `trimSuffix` `hasPrefix` `hasSuffix` `contains` `replace` `split` `join` `repeat` `camelToKebab` `truncate` |
| [02-math.md](02-math.md) | `add` `sub` `mul` `div` `mod` `min` `max` `clamp` `abs` |
| [03-conditional.md](03-conditional.md) | `ternary` `boolTernary` `boolDefault` `default` `coalesce` `empty` `notEmpty` |
| [04-types.md](04-types.md) | `typeOf` `typeMap` `typeList` `typeString` `typeNumber` `typeBool` `typeNull` `Empty` `len` `toInt` `toFloat` `toBool` `toString` |
| [05-cron.md](05-cron.md) | `cronExpr` `cronFromMap` `cronNormalize` `cronDescribe` `cronValid` `cronMinute` `cronHour` `cronDom` `cronMonth` `cronDow` `cronField` |
| [06-random.md](06-random.md) | `randomAlphanumeric` `randomHex` `randomBase64` |
| [07-collections.md](07-collections.md) | `listHas` `listGet` `listLen` `mapGet` `mapKeys` `mapValues` `asList` `asMap` `asString` |
| [08-safe-access.md](08-safe-access.md) | `getOr` `getStringOr` `getIntOr` `getBoolOr` |
| [09-kubernetes.md](09-kubernetes.md) | `meta` `labels` `annotations` `spec` `status` `phase` `get` `ownerKind` `ownerName` `hasCondition` `conditionReason` `conditionMessage` `resourceExists` `isTerminating` `generation` `observedGeneration` `isSynced` |
| [10-container.md](10-container.md) | `containerImage` `containerEnv` `containerPort` |
| [11-quantity.md](11-quantity.md) | `parseQuantity` `formatQuantity` `sumQuantity` |
| [12-replica.md](12-replica.md) | `replicasReady` `readyReplicas` `availableReplicas` `updatedReplicas` `desiredReplicas` |
| [13-job.md](13-job.md) | `jobSucceeded` `jobFailed` `jobActive` |
| [14-service.md](14-service.md) | `serviceClusterIP` `serviceNodePort` `serviceLoadBalancerIP` `serviceLoadBalancerHost` `endpointsReady` |
| [15-fields.md](15-fields.md) | `resourceName` `resourceNamespace` `resourceUID` `resourceVersion` `creationTimestamp` |
| [25-semver.md](25-semver.md) | `semverMajor` `semverMinor` `semverPatch` `semverValid` `semverCompare` `semverBump` `semverConstraint` |
| [26-time.md](26-time.md) | `timeAgo` `timeSince` `isExpired` `timeFormat` `durationSeconds` `durationAdd` `durationValid` |
| [27-data.md](27-data.md) | `toBase64` `fromBase64` `toJSON` `sha256sum` `truncateName` `slugify` |
| [28-git.md](28-git.md) *(in development)* | `gitShortCommit` `gitIsCommit` `repoName` `repoOrg` `repoHost` `repoSSHToHTTPS` `repoHTTPSToSSH` `gitDefaultBranch` `gitRefShort` `gitChanged` |
| [29-docker.md](29-docker.md) *(in development)* | `dockerRegistry` `dockerRepo` `dockerTag` `dockerNoTag` `dockerName` `dockerWithTag` `dockerWithDigest` `dockerCommitTag` `dockerBuildSucceeded` `dockerHasDigest` |

Read the docs for the category you need. For a quick lookup of a specific function name, use the table above as an index.
