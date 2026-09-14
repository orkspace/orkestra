# 02 — NotifyBlock

## Attaching notifications to a condition

A condition carries a `notify:` block that declares which teams receive an alert when the condition passes:

```yaml
conditions:
  - field: metrics.errorRatePercent
    greaterThan: "10"
    notify:
      teams: [ops, sre]
      message: "Error rate has exceeded 10% — check the reconciler logs."
```

`notify.message` is optional. When present it overrides the team's default message template. When absent, the team's configured message (or a generic fallback) is used.

## NotifyBlock type

```go
type NotifyBlock struct {
    Teams   []string `yaml:"teams"`
    Message string   `yaml:"message,omitempty"`
}
```

The `message` lives on the notify block — not on the team definition — because the message should describe *this condition*, not the team. Different conditions notifying the same team can carry different messages.

## Team resolution

For each team in `notify.teams`:

1. Look up `katalog.Notification.Teams[teamName]`. If absent or nil, skip.
2. Check the interval gate via `LastSent[key]`. Skip if within interval.
3. Resolve the message: `cond.Notify.Message` if non-empty, else `k.Notification.EffectiveMessage(teamName)` (where `k` is the `*katalog.Katalog`).
4. Call `dispatchTeamNotifications` — fans out to email and Slack based on team config.

## Message resolution priority

```
cond.Notify.Message                    (condition-level — highest priority)
  └─ katalog.Notification.EffectiveMessage(teamName)
       └─ team.Message                 (team-level default)
            └─ katalog.Notification.Message   (global default)
                 └─ ""                 (no message — body will be Empty()
```

## Condition key

The interval gate key encodes the condition so that different conditions on the same field+team have independent clocks:

```
<katalogName>|<field>|<operator>|<value>|<teamName>
```

This means a condition that changes its threshold (e.g. `greaterThan: "5"` → `"10"`) resets the clock for that team.

---

**Next →** [03 — Channels](03-channels.md)
