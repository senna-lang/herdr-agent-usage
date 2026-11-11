# `usagebar collect --format v1`

A versioned JSON snapshot of the Agent Usage panel, for hosts that draw the
panel themselves (the herdr-app mod under `app/`). The text pane and the
sidebar do not use it.

```sh
usagebar collect --format v1 [--all] [--quotas-only] [--panes JSON|-]
```

- Without `--all` only providers with an open agent pane are kept (the pane's
  default). `--panes` supplies those panes as a JSON array instead of asking
  herdr: `[{"paneId":"p1","agent":"claude","label":"api","cwd":"/repo"}]`
  (`agent` is the herdr agent id; entries without one are ignored; `-` reads
  the array from stdin). An empty array means no agent panes are open.
- `--quotas-only` skips pane shares, pay-as-you-go blocks and cache warnings.
- Without `--format` the command prints the older, unversioned debug shape.

Adding fields is compatible. Renaming or removing one bumps `version`.

```jsonc
{
  "version": 1,
  "generatedAtMs": 1793722431615,
  "filter": "open-panes",            // or "all"
  "limitPercent": "remaining",       // [ui] limit_percent: "remaining" | "used"
  "emptyMessage": "(no agent panes open)",
  "providers": [{
    "providerId": "claude-work",
    "family": "claude",              // the provider family, shared by all its accounts
    "label": "work",                 // the text pane's heading for a lone entry
    "groupLabel": "Claude",          // set when 2+ accounts of a family share a heading
    "accountLabel": "me@example.com",
    "profileId": "claude-work",      // the [[claude.profiles]] entry behind it
    "profileLabel": "work",
    "identity": "me@example.com",    // login email, else the account's config/home dir
    "account": "me@example.com",     // the login (email / account id) when readable, else ""
    "sameAccountAs": "",             // providerId of an earlier entry with the same account:
                                     // show the account once; its windows are shared (freshest wins)
    "planType": "Max",
    "source": "claude statusLine cache",
    "fetchedAtMs": 1793722431615,
    "note": null,                    // the text pane's note line, unchanged
    "status": "ok",                  // ok | stale | needs-login | unavailable
    "staleMinutes": null,            // set with status "stale"
    "action": "",                    // e.g. "grok login" with status "needs-login"
    "windows": [{
      "slot": "primary",             // primary | secondary | tertiary
      "tag": "5h",                   // 5h | 24h | 7d | 30d | <n>m
      "windowMinutes": 300,
      "usedPercentage": 38,
      "remainingPercent": 62,
      "displayPercent": 62,          // the number to print under limitPercent
      "barFill": 0.62,               // 0-1, moves with displayPercent
      "band": "high",                // high >=50% left, mid >=20%, low; follows remaining
      "resetsAt": 1793730000,        // unix seconds, or null
      "resetIn": "2h 14m",           // "" when unknown
      "runOut": { "minutesToEmpty": 38, "emptyBeforeReset": true, "text": "empty in ~38m" }
    }],
    "paneActivity": {                // null with --quotas-only or no activity
      "windowMinutes": 300, "tag": "5h", "totalTokens": 1234,
      "panes": [{ "paneId": "p1", "label": "api", "tokens": 876, "sharePercent": 71 }]
    }
  }],
  "apiUsage": [{
    "backendId": "deepseek", "label": "DeepSeek", "hasCost": true,
    "windows": [{ "windowMinutes": 1440, "tag": "24h", "tokens": 332000, "costUsd": 0.02 }],
    "models": [{ "modelId": "deepseek-chat", "tokens": 332000, "costUsd": 0.02 }],
    "paneActivity": null
  }],
  "lowCachePanes": [{ "label": "Task A", "hitPercent": 35 }]
}
```

## `usagebar notify --json [--panes JSON|-]`

Prints the rate-limit toasts that are due now instead of sending them through
herdr, and records them as shown, so a host can post them itself. It covers
every provider's shortest window and both Claude windows (5h and 7d, per
profile, sharing the statusLine bridge's state, so a crossing fires once):

```json
{ "version": 1, "toasts": [{
  "providerId": "codex", "profileLabel": "personal", "window": "primary",
  "bucket": 20, "resetsAt": 1793730000, "level": "warning",
  "title": "Codex limit · personal",
  "body": "20% remaining · resets in 1h 12m" }] }
```

`level` is `warning` above the 10% bucket and `error` at 10% and below (with
the default thresholds: 50/20 warn, 10/5 error). It honours `[notify] enabled`
and `remaining_thresholds`. To keep herdr from also sending them, set:

```toml
[notify]
delivery = "external"   # default "herdr"
```

With `external`, `usagebar notify` (the herdr hook) and the statusLine bridge
send nothing and leave the crossing for `notify --json`.
