# Devin contract

What this plugin reads from Devin CLI, and what it deliberately does not.

## Tested baseline

| | |
| --- | --- |
| Devin CLI | 3000.11.205 (macOS arm64) |
| Session store | `~/.local/share/devin/cli/sessions.db`, SQLite, opened `mode=ro` with `query_only(1)` |
| Credential store | `~/.local/share/devin/credentials.toml` |
| Herdr integration | `herdr integration install devin`, reporting `agent_session.kind=id` with the session's own name (`bejeweled-whitefish`, …) |
| Overrides | `DEVIN_DATA_DIR` relocates the whole data directory |

Devin CLI keeps every session in one SQLite database it owns, and on an
active machine it grows to tens of gigabytes. The adapter therefore only
ever resolves through indexed single rows — `sessions.id` for membership,
and `message_nodes` descending `row_id` for the newest request — and never
scans a session's node forest on the refresh path. `chat_message` carries
full message payloads (~35KB per node), so any query that parses it across
a session's nodes reads gigabytes and spills temp b-trees on every pane
event.

## Context

Sessions are node forests: `message_nodes.chat_message` holds one chat
message per node, and assistant nodes record their request under
`$.metadata.request_id` with usage under `$.metadata.metrics`:

```
context tokens = metrics.context_input_tokens   (newest row with the field)
```

`context_input_tokens` is the context occupying the window at that request —
fresh input plus cached input — which is exactly the sidebar's `$context`
meter; no replay of the transcript is required.

Recency follows `row_id`, the table's INTEGER PRIMARY KEY: Devin appends
one node per streamed update, so the newest metrics-bearing row is a btree
descent away, not a sort. The cache row (`$cache_*`) falls back to that
same request's own counters.

The context window size is **not reported**: no local surface publishes a
Devin model's context limit, so `WindowTokens` stays unset and the sidebar
shows the bare token count rather than a percentage against a denominator
that does not exist.

### Not aggregated

The same `request_id` appears on several sibling nodes carrying the same
metrics, because Devin writes one node per streamed update of the same
request. Any session-wide aggregate — lifetime tokens, prompt-cache rate
over the session, tokens inside a time window — therefore has to dedupe by
`metadata.request_id`, which lives inside `chat_message` and forces a parse
of every node row. On a multi-GB store that is seconds of CPU and gigabytes
of page traffic per invocation, and pane events arrive faster than such a
query completes, so invocations stack up. These numbers are deliberately
not collected until an incrementally maintained index can serve them: a
watermark over `row_id` could add only new rows per refresh, but building
that index is its own change.

Concretely: `$cache_*` shows the newest request's hit rate rather than the
session's, and the Agent Usage pane shows no token-share row for Devin.

### Session identity

Herdr reports the session's own name as `agent_session.kind=id`, and that
value is the `sessions.id` primary key, so resolution is an exact lookup
with no cwd fallback: two panes in one directory are distinct sessions by
construction.

### Not used

- `sessions.model`, `sessions.metadata`, `sessions.cogs_json` — the model on
  the session row is the launch-time choice and says nothing about what an
  individual request ran; the per-request `generation_model` is read instead
  where a model is needed. `metadata.total_acu_cost` is a plan-unit figure,
  not tokens or dollars.
- `message_nodes.metadata` — mostly NULL, and request ownership lives on
  `chat_message`; the separate metadata column holds node-tree bookkeeping
  where present.

## Quota

Two windows, daily and weekly. Devin meters against percentage-of-plan
quota — the API reports the share each window has **left**, which the
collector inverts into the displayed "used" bar. An over-consumed window
can report a negative remainder; it clamps to fully used rather than
disappearing.

Source: `POST <api_server_url>/exa.seat_management_pb.SeatManagementService/GetUserStatus`,
the Connect-protocol call Devin's own tooling makes for account status —
the same request the OpenUsage app converged on — authenticated with the
`windsurf_api_key` from `credentials.toml`. The key travels inside the
request's `metadata` envelope, not an Authorization header, matching the
shape Windsurf-family clients send.

| Field | Used as |
| --- | --- |
| `userStatus.planStatus.planInfo.planName` | plan label (`Pro`, …) |
| `userStatus.planStatus.planInfo.hideDailyQuota` | hides the daily window on plans that do not meter it |
| `userStatus.planStatus.dailyQuotaRemainingPercent` | daily share left → used = 100 − left |
| `userStatus.planStatus.weeklyQuotaRemainingPercent` | weekly share left → used = 100 − left |
| `userStatus.planStatus.dailyQuotaResetAtUnix` / `weeklyQuotaResetAtUnix` | reset countdown (proto3 int64, spelled as a JSON string) |
| `userStatus.planStatus.overageBalanceMicros` | extra-usage wallet, USD millionths — a balance, never a percentage |

The daily window occupies the primary slot and the weekly the secondary. A
plan that hides its daily window (`hideDailyQuota`) shows only the weekly;
when such a plan still meters a daily share and has no weekly window, the
daily share takes the secondary slot — a metered window is better shown
under the other label than not shown at all.

proto3 JSON drops zero-valued fields, so an absent field is distinguished
from a real zero: a window whose share is absent while its reset is present
reads as exhausted (nothing left is what a dropped zero means), while an
absent `overageBalanceMicros` means the plan has no wallet at all and no
note is drawn. The wallet is a dollar balance with no window, so it is
stated in the note (`extra usage balance -$0.32`) rather than metered —
the same rule the other collectors apply to facts with no denominator.

### Failure semantics

The two failure kinds are kept apart, matching the Kilo collector. A request
that fails — transport, auth, decode — records the attempt in its own right
but leaves the last good snapshot on disk: a blip is not evidence that the
quota changed. A still-fresh failed entry replays **its own** failure — no
window, and the note naming what went wrong — never the snapshot it
preserved, which stays for a later attempt to recover from.

The endpoint is the `api_server_url` the login itself recorded (default
`https://server.codeium.com`), required to be https, and redirects are never
followed: a credential-bearing request cannot be replayed to a host this
collector did not choose. Results are cached for 2 minutes (10 after a
failure), keyed to the login's hashed identity, so a different account never
sees numbers its predecessor produced.

### Deliberately not collected

- **Rate limits.** The seat response carries no per-minute or per-request
  bucket, and none is inferred.
- **ACU spend.** Sessions meter in plan units; no local surface prices a
  request in dollars, so no cost column is fabricated.
- **Context window size.** No local or remote surface in this contract
  names a model's context limit; see Context.

## Identity

Devin CLI has one login per machine and one billing route — every session
meters against the signed-in account's plan quota, with plan overage still
subscription-class spend — so there is no per-session backend evidence to
classify and no `SessionBillingProvider` implementation. `paneBillingModeWith`
maps the provider id to subscription directly.

## Credentials

The API key is read for one HTTP request and never persisted: the cache
entry stores only the resolved limits plus `sha256("devin\000" + key)`, so a
stale cache file cannot leak the login and a second account's entry is
refused rather than displayed.
