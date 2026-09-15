# Antigravity CLI contract

The tested baseline for Antigravity CLI (`agy`) support, recorded for drift
monitoring.

## Tested version

| | |
| --- | --- |
| Antigravity CLI | `1.2.3` (Homebrew cask `antigravity-cli`) |
| Verified on | macOS, `~/.gemini/antigravity-cli` default app-data directory |

## Where the contract is documented

Antigravity's statusLine payload is **not documented on any public docs
site**. It is exposed only through `/statusline <command>` inside the CLI
itself (`agy` → `/statusline help`), which spawns the configured command on
every status update and pipes it the same kind of JSON session description
Cursor's statusLine uses. There is therefore no URL for the drift checker to
watch for this contract; it is pinned instead by the captured-payload fixture
in `internal/providers/antigravity/statusline_test.go`, which must be
re-captured when the tested version advances.

Antigravity ships an `agy 1.2.2`-era `history.jsonl` / `presence/` layout in
some installs and a `conversation_summaries.db` / `cache/conversation_metadata.json`
layout in others (observed switching between a same-day reinstall at `1.2.3`).
This plugin depends on neither: see "Not used" below.

## Payload semantics relied upon

From the statusLine payload, this provider reads:

- `conversation_id` — the session identity. herdr's `antigravity_cli`
  integration reports this as `agent_session` with **`kind: "id"`**, not
  `"path"` — confirmed against a real `herdr pane get` while `agy` ran inside
  a herdr pane. This is the only currently-registered provider using `kind:
  "id"` rather than a session file path.
- `context_window.total_input_tokens` — a running, non-nullable integer.
  Unlike Cursor, Antigravity never nulls this field out early in a session; a
  captured `0` is a real "no turns yet" observation, not a missing one.
- `context_window.context_window_size` — the active window, reported directly
  (no static model→window table is needed).
- `context_window.current_usage.{input_tokens,cache_creation_input_tokens,cache_read_input_tokens}`
  — the latest completed turn's cache breakdown, present only after the first
  turn. Feeds `$cache_*`; there is no session-cumulative cache counter
  locally, so `SessionCache` is intentionally left unset.
- `quota` — a map of weekly allotments, keyed by Antigravity's own bucket id.
  Two buckets were observed: `gemini-weekly` (Antigravity's native model) and
  `3p-weekly` (third-party models — Claude, GPT, … — routed through it), each
  carrying `remaining_fraction` (0-1) and an absolute `reset_time` (RFC3339).
  `reset_in_seconds` is also present but not used: it is only accurate at
  capture time, while `reset_time` is an absolute instant.
- `plan_tier` — a human label for the account's plan (e.g. `"Antigravity
  Starter Quota"`), shown as `$provider`'s plan type.
- `transcript_path` — recorded by herdr's hook as `agent_session_path`
  alongside `agent_session_id`; not currently read by this provider, since the
  statusLine payload already carries everything needed.

## Not used

- **`~/.gemini/antigravity-cli/conversations/*.db`** — one SQLite file per
  conversation, with `gen_metadata`/`steps`/`executor_metadata` payload
  columns that are protobuf-encoded. Whether per-turn token counts live in
  there was never confirmed, and is moot: the statusLine payload already
  reports the same figures directly, in a stable JSON shape, with no schema
  reverse-engineering or SQLite dependency required.
- **`history.jsonl` / `presence/*.lock`** — used by some installs to map a
  conversation id to a workspace path; unneeded because herdr's own
  `agent_session` already carries the conversation id directly (`kind:
  "id"`), and the statusLine payload carries it a second time.

## Identity semantics

Antigravity mints a new conversation id when a session is cleared or resumed
into a new conversation, while herdr keeps reporting the one observed when the
pane launched — the same failure mode Cursor's provider documents
(herdrdev/herdr#2510) and handles the same way: snapshots are stored by
conversation id but also record the herdr pane id, and resolution falls back
to pane identity when the reported id's snapshot is stale or has been
superseded by a newer one on the same pane. Working directory alone is not
sufficient: two Antigravity panes may share one repository.

## Quota semantics

Unlike Cursor, Antigravity's statusLine reports account-wide quota directly,
so it is registered `CapOwnsSubscriptionQuota` rather than `CapContextOnly`.
Quota is billed per Google account, not per conversation: the limits collector
reads whichever snapshot is freshest across every known session, not one tied
to a specific pane, since any pane's observation of a shared account speaks
for the whole account.

`gemini-weekly` is shown as the sidebar `$limit` row's primary window (the
account's own native quota); `3p-weekly` as secondary. Both share the same
one-week duration, so — unlike Claude's 5h/7d split — the primary/secondary
assignment is a display convention (native quota first), not a duration
ranking.
