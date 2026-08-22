# Cursor CLI contract

The tested baseline for Cursor support, recorded for drift monitoring.

## Tested version

| | |
| --- | --- |
| Cursor CLI | `2026.08.11-e8db854` |
| Verified on | macOS, `~/.cursor` default config directory |

## Where the contract is documented

Cursor's statusLine payload is **not documented on the public docs site**. It is
documented in the `statusline` skill bundled with the CLI itself
(`<config dir>/skills-cursor/statusline/SKILL.md`), which ships and versions with
the binary. There is therefore no URL for the drift checker to watch for this
half of the contract; it is pinned instead by the captured-payload fixture in
`internal/providers/cursor/statusline_test.go`, which must be re-captured when
the tested version advances.

The configuration-directory half *is* public and is watched by
`scripts/contractdrift` (`provider-contracts.json`, id `cursor`):
<https://cursor.com/docs/cli/reference/configuration>.

Note that the npm package `cursor-agent` is **not** Cursor's CLI — it is an
unrelated third-party package. Cursor's CLI is distributed by Cursor's own
installer, so it has no npm baseline in `contracts.json`.

## Payload semantics relied upon

From `context_window`, this provider reads `total_input_tokens` and
`context_window_size` and nothing else.

`total_input_tokens` is documented by Cursor as *"Estimated input tokens (derived
from `used_percentage`)"*. A captured payload confirms the derivation exactly:

```
used_percentage / 100 × context_window_size = 16.7 / 100 × 256000 = 42751.99…
total_input_tokens                                                 = 42752
```

Two consequences follow:

- The two fields cannot disagree; one is computed from the other. Occupancy is
  therefore quantised to one decimal place of the window — 0.1%, about 256
  tokens in a 256k window. That is finer than the `$context` row renders, which
  rounds to whole percent.
- `used + remaining` sums to exactly 100.

`context_window.current_usage` is deliberately **not** used. It describes the
last API call rather than the current window, is absent before the first call,
and only approximates occupancy on a turn that happened to read the entire prior
context from cache. In the captured payload its
`input + cache_creation + cache_read` sums to 42825 against a reported 42752 —
close, but not the same quantity.

## Identity semantics

Cursor mints a new `session_id` when a conversation is cleared, while herdr keeps
reporting the `agent_session` observed when the pane launched
(herdrdev/herdr#2510). Snapshots are therefore stored by session id but also
record the herdr pane id, and resolution falls back to pane identity. Working
directory alone is not sufficient: two Cursor panes may share one repository.

## Not available locally

Cursor records no rate-limit or plan-window data on disk, and its usage endpoints
are server-side. Cursor is registered `CapContextOnly` for that reason: it
populates `$context`, leaves `$limit` empty, and contributes no Agent Usage panel
block and no rate-limit notifications.
