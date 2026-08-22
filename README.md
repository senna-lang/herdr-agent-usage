# Agent Usage

[![CI](https://github.com/senna-lang/herdr-agent-usage/actions/workflows/ci.yml/badge.svg)](https://github.com/senna-lang/herdr-agent-usage/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
![Go 1.25+](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)
![herdr 0.7.5+](https://img.shields.io/badge/herdr-0.7.5%2B-6E56CF)
![platforms: linux | macOS](https://img.shields.io/badge/platforms-linux%20%7C%20macOS-lightgrey)

Monitor context usage and provider rate limits for agents running in [Herdr](https://herdr.dev).

![Agent Usage pane showing Claude, Codex, OpenCode Go, and Grok subscription limits alongside a pay-as-you-go API spend block (DeepSeek shown as one example), with per-pane activity shares](docs/assets/agent-usage-pane.png)

- **Per-pane context meters** — every agent pane's sidebar label shows how much of its context window the session is using (`⛁ 13% (130k)` = 130k tokens, 13% of the window), updated after each completed turn.
- **Provider limit row** — a separate sidebar row shows the shortest account-limit window (`5h 72%`) without crowding the context meter.
- **Account rate-limit windows at a glance** — one live pane shows how much 5h / 7d / 30d allowance is left for Claude, Codex, OpenCode Go, and Grok, with reset countdowns and which open pane is burning it.
- **Low-allowance warnings** — optional toasts fire when a window drops below your thresholds (default 50 / 20 / 10 / 5 % left), before you hit the wall mid-task.
- **Pay-as-you-go API backends** — when a pane runs a direct API key instead of a subscription, there's no plan quota to show. Works with **any** provider a harness can reach (DeepSeek, OpenAI, Together, OpenRouter, Ollama, Bedrock, Vertex, a custom gateway, …), not just the one the screenshot happens to show. Detected across supported harnesses from what each records locally: OpenCode's per-message `providerID`, Codex's `model_provider`, Claude's deployment env (Bedrock / Vertex / Foundry / gateway), Grok custom models (`~/.grok/config.toml` `[model.*]` `base_url`), and OMP / Pi assistant `message.provider`. The sidebar shows the pane's current backend and what that backend spent in the session (e.g. `deepseek · Σ 425k $0.04`); the Agent Usage pane adds a per-backend block with rolling 24h / 7d / 30d totals for that provider, a per-model breakdown, and which open pane is spending it. Dollar cost is shown when the harness records it (OpenCode, OMP, and Pi today); otherwise the block is token-only.

## Requirements

- **Herdr ≥ 0.7.5**
- **macOS or Linux**
- Agent integrations for reliable session matching (recommended):

```bash
herdr integration install codex
herdr integration install opencode
herdr integration install omp
herdr integration install pi
# Claude Code integration recommended when you use Claude panes
```

## Install

```bash
herdr plugin install senna-lang/herdr-agent-usage
# non-interactive shells (CI, coding agents) need --yes
```

Plugin install does **not** rewrite `~/.config/herdr/config.toml` (sidebar rows, toast delivery, keybindings). Run setup after install:

```bash
herdr plugin action invoke usagebar.setup
# optional: append toast delivery if missing
herdr plugin action invoke usagebar.enable-toast
herdr server reload-config
```

`herdr plugin install` provisions the `usagebar` binary automatically as part of install/update (via the manifest's `[[build]]` hook): it builds with the local Go toolchain (≥ 1.25) when available, and otherwise downloads a prebuilt binary from [GitHub Releases](https://github.com/senna-lang/herdr-agent-usage/releases) (macOS / Linux, arm64 / amd64). `usagebar.setup` repeats this resolution as a fallback for installs predating the build hook. To build manually instead, run `make build` in the plugin root.

## Let an LLM set it up

Copy the prompt in [docs/LLM-SETUP.md](docs/LLM-SETUP.md) into an LLM coding agent.
The agent can install the plugin and guide you through the remaining setup.

- **Toasts:** The agent must ask for your approval before enabling toast notifications.
- **Keybindings:** The recommended shortcuts are `ctrl+shift+u` to open the limits pane and `ctrl+shift+m` to refresh meters (single chords; no Herdr prefix). If either shortcut is already in use, the agent must ask which shortcut to use instead.

## Quick start

1. Install the plugin and run **setup** (above).
2. Open a workspace with at least one agent pane.
3. Add the sidebar rows printed by `usagebar.setup` to your Herdr config, then run `herdr server reload-config`.
4. After an agent turn completes (or you focus the pane), the sidebar shows provider limit remaining above context usage. `$limit` also refreshes on its own while the pane sits idle. A Herdr restart restores the same tokens via the plugin startup hook.
5. Open the limits pane:

```bash
herdr plugin action invoke usagebar.open-limits
```

6. Optional keybindings in **your** `~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = "ctrl+shift+u"
type = "plugin_action"
command = "usagebar.open-limits"
description = "Agent Usage: open limits pane"

[[keys.command]]
key = "ctrl+shift+m"
type = "plugin_action"
command = "usagebar.refresh"
description = "Agent Usage: refresh sidebar meters"
```

On Mac that is **Control+Shift+U** / **Control+Shift+M** (not Command). Then `herdr server reload-config`.

## Actions

| Action | Command | What it does |
| --- | --- | --- |
| Open limits pane | `usagebar.open-limits` | Split pane with provider windows |
| Refresh meters | `usagebar.refresh` | Recompute sidebar `$limit` and `$context` tokens for the target pane |
| Setup | `usagebar.setup` | Seed plugin config, show sidebar/toast/key snippets, report Herdr toast status |
| Enable toast | `usagebar.enable-toast` | Append `[ui.toast]` only if missing (never overwrites) |
| Check for updates | `usagebar.check-updates` | Check GitHub Releases now and show the release/update instructions |

```bash
herdr plugin action list --plugin usagebar
herdr plugin action invoke usagebar.setup
```

## What you get

| Surface | What it shows |
| --- | --- |
| **Sidebar `$context` row** | Per-pane context usage: `⛁ 13% (130k)` when the window size is known, or the token count alone |
| **Sidebar `$limit` row** | Shortest provider limit window (`5h 72%` remaining), refreshed with the Agent Usage pane (15s) or every 60s while that pane is closed. Pay-as-you-go panes show what that pane spent on its backend (`Σ 425k $0.04`, scoped to the pane's session and backend) instead |
| **Sidebar `$provider` row** | Subscription provider (`opencode-go`, `grok`, `claude`, …) on a subscription pane, or the backend actually billed on a pay-as-you-go pane (`deepseek`). The adjacent burn total is scoped to that same backend in the pane's session |
| **Agent Usage pane** | One block per billing provider, independent of harness. Subscription providers show plan windows and cross-harness pane activity. Pay-as-you-go backends show one merged 24h / 7d / 30d block, model breakdown, and pane share even when multiple harnesses use the same backend |
| **Toasts** (optional) | Remaining-limit warnings at configured thresholds (default 50 / 20 / 10 / 5 % left) |

### Supported agents

| Agent | Sidebar context + limit | Limits pane | Notes |
| --- | --- | --- | --- |
| Claude Code | Yes | Yes | Subscription windows from `~/.claude.json` / statusLine cache. Pay-as-you-go (API key, Bedrock, Vertex, Foundry, gateway) hides those windows and labels the backend from deployment env / settings |
| Codex | Yes | Yes | Context + rate windows from local rollouts; custom `model_provider` panes are pay-as-you-go |
| OpenCode | Yes | Yes | The `opencode-go` subscription keeps no usage numbers on disk, so its windows come from opencode.ai, authenticated by `OPENCODE_GO_COOKIE` or a browser session imported via the Keychain ([details](#opencode-go-official-usage)); without either it degrades to a local SQLite estimate. Other backends (e.g. DeepSeek) show token/cost spend instead of plan windows |
| Grok | Yes | Yes | Context from `signals.json`; SuperGrok credits when auth is present. Custom models (`~/.grok/config.toml` `[model.*]` with `base_url`) are pay-as-you-go and labelled from the endpoint host (openai, ollama, …) |
| OMP (Oh My Pi) | Yes | Yes | Session jsonl plus its credential metadata. Subscription routes: OpenCode Go, Grok OAuth, Anthropic OAuth → Claude, and OpenAI Codex OAuth → Codex. API-key backends show backend-scoped session burn |
| Pi coding agent | Yes | Yes | Session jsonl plus `~/.pi/agent/auth.json`; context windows come from Pi's `models-store.json` / `models.json`, and session trees plus compaction boundaries are respected. Uses the same recognized OAuth/subscription routes and pay-as-you-go rules as OMP |

Percentages in the limits pane are **remaining** (`% left`). Higher is safer.

## Agent Usage pane

- Auto-refreshes every **15s**, and the same collect updates sidebar `$limit` on every open subscription pane. Press **`r`** to refresh, **`q`** to quit. With the pane closed, `$limit` still moves every **60s**. `$context` stays event-driven after the initial restore. After a Herdr restart or live handoff, a `[[startup]]` hook republishes `$title` / `$provider` / `$limit` / `$context` for every open agent pane so the sidebar is not blank until the next focus or turn.
- OpenCode Go may show three windows (**5h / 7d / 30d**). Other providers show whichever usage windows their data sources make available.
- Open pane **token share** is local activity share within the shortest window (including a **closed / other** bucket for usage outside open panes). It is not account quota attribution.
- Sidebar meters update after the agent has **settled** (not while `working`), so they match the last completed turn. If the session cannot be resolved, the `$context` token is cleared rather than showing another session’s numbers.
- After a Claude Code **compaction**, the meter shows `⛁ compacted (14k)` — the boundary’s own post-compaction estimate — instead of the stale pre-compact size, until the next completed turn reports real usage again.

```bash
herdr plugin action invoke usagebar.open-limits
```

## Configuration

### Sidebar rows (Herdr 0.7.4+)

Add `$title`, `$provider`, and `$limit` as their own rows so the existing
context text remains unchanged:

```toml
[ui.sidebar.agents]
row_gap = 0
rows = [
  ["state_icon", "$title"],
  ["$provider", "$limit"],
  ["$context"],
]
```

`$title` replaces Herdr's built-in `tab`/`pane` tokens, which render blank
whenever a tab still carries its auto-assigned numeric label (e.g. a
never-renamed tab 1 reports `"1"`) and the pane itself was never renamed.
`$title` falls back to the workspace ("space") name in that case, then
appends a pane rename when present: `tab` → `space` → `space・pane`.

`$provider` replaces Herdr's built-in `agent` token: it renders the quota
provider (`opencode-go`, `grok`, `claude`, …) on a subscription pane, and the
backend (`deepseek`) on a pay-as-you-go pane. Herdr joins row tokens with `·`
and has no separator setting, so the harness and billing identity are not
shown side by side. This makes the standard display `title`,
`provider · limit`, then context. Run `herdr server reload-config` after
editing. The limit disappears automatically when the matching provider has no
limit data.

### Plugin config

```bash
herdr plugin config-dir usagebar
# → ~/.config/herdr/plugins/config/usagebar/config.toml
```

Created on first `usagebar.setup` (or when missing):

```toml
[notify]
enabled = true
remaining_thresholds = [50, 20, 10, 5]
```

`enabled = false` suppresses all Agent Usage toasts, including remaining-limit
warnings and update-available notices; statusLine summaries and cached limits
continue to refresh. `remaining_thresholds` accepts remaining percentages from
1 through 100 (for example `[30, 10]`); each threshold can notify once per
window, from least to most severe.

### Multiple Claude accounts

Add one `[[claude.profiles]]` block per account to the plugin config. Each
profile owns its own limits cache, notify state, and transcript root under its
`config_dir`, so accounts never share readings. With no profile configured the
plugin tracks a single account at `~/.claude` (unchanged behavior).

```toml
[[claude.profiles]]
id = "base"                    # provider id; must be unique
label = "personal"             # optional, shown in the pane and toasts
config_dir = "~/.claude"       # the default account

[[claude.profiles]]
id = "work"
config_dir = "~/.claude-work"  # started via CLAUDE_CONFIG_DIR=~/.claude-work claude
```

- **Declare the default account too.** Bare `claude` sets no
  `CLAUDE_CONFIG_DIR` — the convention is to set it only for *additional*
  accounts — so once any profile exists, the account at `~/.claude` needs an
  entry of its own to be recorded.
- `config_dir` may use `~`; it is expanded and each account is matched to its
  profile by the resolved path. `claude_json_path` is optional and only needed
  for a non-default `.claude.json` location.
- A statusLine invocation whose config dir matches no profile writes nothing
  and names the mismatch on stderr rather than attributing usage to the wrong
  account. `usagebar setup` lists the resolved profiles and warns when an entry
  was ignored or the default account is uncovered.

### Multiple Codex accounts

Add one `[[codex.profiles]]` block per `CODEX_HOME` to the plugin config. Each
profile is collected from that home's rollouts and `auth.json`, so accounts
never share readings. With no profile configured the plugin tracks a single
account at `~/.codex` (unchanged behavior).

```toml
[[codex.profiles]]
id = "codex"                   # provider id; must be unique
label = "personal"             # optional, shown in the pane
codex_home = "~/.codex"        # the default account

[[codex.profiles]]
id = "dev"
label = "product"
codex_home = "~/.codex-dev"    # started via CODEX_HOME=~/.codex-dev codex
```

- **Declare the default account too.** Bare `codex` sets no `CODEX_HOME` — the
  convention is to set it only for *additional* accounts — so once any profile
  exists, the account at `~/.codex` needs an entry of its own to be recorded.
- `codex_home` may use `~`; it is expanded and each pane is matched to its
  profile by the session file under that home. An unresolved multi-profile
  pane is left unassigned rather than counted against the wrong account.
- `usagebar setup` lists the resolved profiles and warns when an entry was
  ignored or the default account is uncovered.

### Multiple Grok accounts

Add one `[[grok.profiles]]` block per `GROK_HOME` to the plugin config. Each
profile reads only that home's `auth.json` and session artifacts. With no
profile configured the plugin keeps the existing single-account behavior.

```toml
[[grok.profiles]]
id = "grok"
label = "personal"
grok_home = "~/.grok"          # the default account

[[grok.profiles]]
id = "grok-work"
label = "work"
grok_home = "~/.grok-work"     # start Grok with GROK_HOME=~/.grok-work
```

- Once any profile exists, declare the default `~/.grok` account explicitly.
- `grok_home` may use `~`. In multi-profile mode, a pane is assigned only when
  exactly one configured home contains its session; unknown or duplicate
  matches are left unassigned.

### Multiple OpenCode accounts

Add one `[[opencode.profiles]]` block per OpenCode data directory. Each
profile reads only its `opencode.db`; accounts never share session, token, or
context attribution. With no profile configured the plugin uses
`$XDG_DATA_HOME/opencode`, or `~/.local/share/opencode`.

```toml
[[opencode.profiles]]
id = "opencode"
label = "personal"
data_dir = "~/.local/share/opencode"  # the default account

[[opencode.profiles]]
id = "opencode-work"
label = "work"
data_dir = "~/.local/share/opencode-work"
```

- Once any profile exists, declare the default data directory explicitly.
- `data_dir` may use `~`. In multi-profile mode, a pane is assigned only when
  exactly one configured database contains its session; unknown or duplicate
  matches are left unassigned.

### Herdr toast delivery

Required for notifications to appear on screen:

```bash
herdr plugin action invoke usagebar.enable-toast
herdr server reload-config
```

Or paste manually into `~/.config/herdr/config.toml`:

```toml
[ui.toast]
delivery = "herdr" # or "system" / "terminal"

[ui.toast.herdr]
position = "bottom-left"
```

`usagebar.enable-toast` **appends only when `[ui.toast]` is missing**. Existing toast settings are left alone.

### OpenCode Go official usage

**OpenCode Go is the only supported subscription with no local source of usage
truth, which is why it — and only it — needs the Keychain step below.**

Every other provider's harness writes the server's own limit numbers to disk,
so this plugin just reads them back:

| provider | local source of truth |
| --- | --- |
| Claude | `~/.claude.json` `cachedUsageUtilization` (+ statusLine cache), or another agent's observation of the same account |
| Codex | `rate_limits` inside `event_msg` / `token_count` in the rollout jsonl, or another agent's observation of the same account |
| Grok | agent stdio / `x.ai` billing, or another agent's observation of the same account |
| **OpenCode Go** | **none of its own** (an observation by another agent still counts) |

`opencode.db` has no usage table at all, its `account` table is empty (auth is
an API key in `auth.json`), and the OpenCode CLI never persists limit state.
There is nothing on the machine to read. So the official 5h / 7d / 30d
percentages can only come from opencode.ai over the network, and that request
has to be authenticated — hence a browser session, and hence the Keychain,
which is where the browser keeps the key to its own cookies.

Anything else for this provider is an estimate, and an estimate cannot be made
correct here: the amounts recorded locally are list-price arithmetic, not what
opencode-go bills against the plan, and no set of caps reproduces the published
percentages across all three windows.

Two ways to authenticate that fetch, tried in this order:

1. `OPENCODE_GO_COOKIE` — an explicit Cookie request header, which always wins:

   ```bash
   export OPENCODE_GO_COOKIE='auth=…'
   ```

2. **A local browser session**, imported automatically — the zero-setup path,
   and the reason the Keychain is involved. If you are signed in to opencode.ai
   in Chrome, Arc, Brave, Edge, or Chromium, the plugin reads that profile's
   cookie for `opencode.ai` and reuses it. Chromium stores cookie values
   encrypted, with the key held in the login Keychain as
   `<Browser> Safe Storage`, so reading the cookie means reading that one
   Keychain item:

   - macOS asks for access the first time. Choose *Always Allow* to keep later
     refreshes silent.
   - The Keychain is only consulted for a profile that actually has an
     opencode.ai cookie, so browsers where you are not signed in never raise a
     prompt.
   - The cookie database is opened read-only (`immutable=1`); nothing is ever
     written back to the browser.
   - The cookie itself is never persisted by this plugin. Only the resolved
     usage snapshot is cached (2 min; 10 min after a fetch failure, 30 s when no
     session was found so a fresh sign-in takes effect promptly).
   - Opt out entirely with `USAGEBAR_DISABLE_BROWSER_COOKIES=1`, and use option
     1 instead if you would rather paste a header than grant Keychain access.

Without either, usage falls back to an estimate from local
`~/.local/share/opencode/opencode.db` (5h rolling, UTC week, calendar month),
labeled `est.` in the panel note. Be aware that this estimate only sees spend
recorded by the OpenCode CLI itself: sessions billed to the same subscription
through another harness (OMP / Pi) are not in that database, so a pane can look
idle while the real window is nearly full.

To see which stage is working:

```bash
usagebar opencode-check
```

It prints the browser profiles found, which profile supplied the session (cookie
names only, never values), and the fetched windows.

### Claude statusLine (optional)

For Claude 5h / 7d windows and toasts, pipe the status line through this plugin. Chain with an existing command rather than replacing it.

```json
{
  "statusLine": {
    "type": "command",
    "command": "bash /path/to/herdr-agent-usage/bin/run-statusline.sh"
  }
}
```

After install, resolve the path with `herdr plugin list` (plugin root under Herdr’s config). `usagebar.setup` prints a ready-to-paste command when `HERDR_PLUGIN_ROOT` is available.

## Rate-limit alerts

Thresholds fire once per window at the configured remaining levels (default **50% / 20% / 10% / 5% left**).

1. Enable toast delivery (`usagebar.enable-toast` or manual snippet).
2. **Claude** — statusLine (above) caches utilization and notifies.
3. **Codex / OpenCode / Grok** — after a settled agent turn, the plugin can display a toast based on the shortest available limit window without the Claude status line.

## Releasing (maintainers)

1. Update `version` in `herdr-plugin.toml`, commit it, and push `main`.
2. Run `scripts/release.sh vX.Y.Z` from a clean, up-to-date `main` checkout.

The script waits for CI on that exact commit before it creates and pushes the
tag. The tag-triggered Release workflow repeats vet, build, test, formatting,
lint, and vulnerability checks before it creates a GitHub Release.

## Upstream drift monitoring

Agent Usage reads implementation details owned by the supported harnesses:
session JSONL, SQLite schemas, auth credential kinds, model catalogs, and
subscription-limit response shapes. Those are contracts in practice, but they
are not stable APIs.

Two scheduled workflows keep those dependencies visible:

- **Model drift** compares the Claude context-window resolver with LiteLLM's
  model catalog.
- **Upstream contract drift** compares the latest official npm releases of
  Claude Code, Codex CLI, OpenCode, Grok Build, OMP, and Pi with the versions
  recorded in `scripts/contractdrift/contracts.json`. It also checks the
  provider-owned limit semantics recorded in
  `scripts/contractdrift/provider-contracts.json` against official Claude,
  Codex, OpenCode Go, and Grok pages. These semantic checks cover the concepts
  the implementation depends on (window units, used/left meaning, shared
  provider ownership, and subscription/API separation), not brittle whole-page
  hashes.

A harness version difference opens or updates one audit issue. It does not
claim the plugin is broken. The issue lists the exact local/session/auth/limit
contracts to recheck. After running the sanitized fixture tests and an
authenticated live-pane smoke test, update the implementation if necessary or
advance that harness's `testedVersion`. Provider responses that require a real
subscription cannot be safely exercised by public CI, so the live smoke step
remains mandatory before advancing those baselines.

There are therefore three drift gates: a harness release signal, public
provider-semantic assertions, and an authenticated live response check. The
last gate is intentionally manual because CI must not contain personal
subscription credentials. In particular, OpenCode Go's unauthenticated local
fallback currently assumes fixed 5h / 7d / 30d USD caps; those constants must
be revalidated whenever the published Go quota model changes.

## Data handling

Everything is computed from files that the agents already keep on your machine:

| Harness | Local sources read |
| --- | --- |
| Claude Code | `~/.claude.json`, statusLine cache under `~/.claude/herdr-usagebar/`, `settings.json` (deployment env) |
| Codex | rollout files under `~/.codex/sessions/` |
| OpenCode | `~/.local/share/opencode/opencode.db` (session usage), `~/.local/share/opencode/auth.json` (credential kind only), and — for OpenCode Go's official windows — the `opencode.ai` cookie in a local Chromium profile plus that browser's Keychain "Safe Storage" password (read-only, never persisted; see [OpenCode Go official usage](#opencode-go-official-usage)) |
| Grok | `~/.grok/sessions/**/signals.json`, `~/.grok/auth.json` (credentials for the credits fetch), `~/.grok/config.toml` (custom-model base URLs) |
| OMP | `~/.omp/agent/sessions/**/*.jsonl`, `~/.omp/agent/models.db` (context window lookup), `~/.omp/agent/agent.db` (credential kind, and the `usage_history` windows OMP records for the accounts it drives) |
| Pi coding agent | `~/.pi/agent/sessions/**/*.jsonl`, `~/.pi/agent/models-store.json` and `~/.pi/agent/models.json` (or the matching `PI_CODING_AGENT_DIR`), `~/.pi/agent/auth.json` (credential kind only) |

Pay-as-you-go detection is not tied to any one harness: it reads the same
per-harness files above (the backend a session used is already recorded there —
OpenCode's `providerID`, Codex's `model_provider`, Claude's deployment env,
Grok's `config.toml`, OMP/Pi `message.provider`). No extra data sources; the
only network calls are the authenticated provider usage fetches (Grok credits,
OpenCode Go usage), and each one is skipped when its credential is absent.

### Where a window comes from

A rate-limit window belongs to the **account**, not to the agent that observed
it. Every collector already treats windows as account-wide, so the plugin
treats the observer as interchangeable too: when a provider's own artifacts
are absent, the account's newest observation by any agent on the machine is
used instead.

That is what makes a subscription driven entirely through another harness
report real numbers. A Claude Team account driven by OMP writes no
`~/.claude.json` utilization and no statusLine cache, because the Claude CLI
never runs — but OMP records the same windows in `~/.omp/agent/agent.db`
(`usage_history`), and those are the account's windows regardless of who
asked for them.

Borrowing is deliberately conservative:

- **The freshest reading wins, not the closest one.** A provider's own
  artifacts are consulted first, but `~/.claude.json` and a Codex rollout are
  caches with a timestamp, not live truth. When another agent observed the
  same window more recently, that number is shown — otherwise an idle CLI's
  old snapshot would under-report usage right as a limit approaches. Live
  authenticated fetches (Grok, OpenCode Go) are by definition current, so
  nothing outranks them.
- **The account must match.** If identities are known and none is the pane's
  account, nothing is borrowed — showing another account's numbers is worse
  than showing none. When no observer named the account, a borrow happens only
  if exactly one account was observed for that provider.
- **A borrowed row says so.** It carries the account (or `account unverified`),
  the observer, and the age of the observation, e.g.
  `account you@example.com · via OMP · ~3m ago`.

A combination with no observer left is reported honestly rather than guessed:
Pi never persists windows of its own, so a Claude or Codex account used
exclusively through Pi, on a machine where neither the vendor CLI nor OMP has
ever touched that account, still shows its "no data" note. Grok and OpenCode Go
are unaffected either way — their usage fetches are authenticated over the
network and need no local observer at all.

### Harness and billing identity

The agent running in a pane and the account paying for a turn are separate.
The plugin resolves a session's backend **and authentication kind**, then uses
the matching quota collector. Today the supported subscription routes are:

| Harness | Session provider + auth | Limit account shown |
| --- | --- | --- |
| Claude Code | Claude login | Claude |
| Codex | ChatGPT login | Codex |
| OpenCode | `opencode-go` | OpenCode Go |
| OpenCode | OpenAI / Codex OAuth | Codex |
| OpenCode | `xai-oauth` | Grok |
| OpenCode | `anthropic` + OAuth | Claude |
| OMP / Pi | `opencode-go` | OpenCode Go |
| OMP / Pi | `xai-oauth` | Grok |
| OMP / Pi | `anthropic` + OAuth | Claude |
| OMP / Pi | `openai` / `openai-codex` + OAuth | Codex |

The same provider id with an API key is pay-as-you-go, so it is never routed
to a subscription limit by name alone. A subscription whose collector is not
implemented (for example Copilot) is intentionally not presented as API
spend; add its collector and an explicit route first. The two OpenCode rows
for Grok and Claude fire only when OpenCode's own `auth.json` holds a real
OAuth credential for that provider; OpenCode's documentation does not offer
those subscriptions, so most installs never hit them. A credential filed
under one provider is never accepted as evidence for another.

The Usage pane keys both subscription and API blocks by billing provider, not
by harness. For example, OpenCode and OMP using OpenCode Go produce one
`OpenCode · Go` block; OpenCode and OMP using the same DeepSeek API produce one
merged `DeepSeek · API` block. Harness-specific code is limited to reading its
session and credential formats before emitting the common provider identity
and usage observations.

Network requests happen in the following cases:

- `opencode.ai` — only when a session is available: `OPENCODE_GO_COOKIE`, or an `opencode.ai` cookie in a local Chromium profile. Results are cached for 2 minutes (10 after a failure), so a sidebar refresh does not mean a request. Disable with `USAGEBAR_DISABLE_BROWSER_COOKIES=1`
- `grok.com` — only when `~/.grok/auth.json` exists (you ran `grok login`)
- `api.github.com` — on the first pane focus and then at most once every 24 hours, to check this plugin's latest public release. The request has no credentials and sends no usage or session data.

No telemetry, no analytics, or usage/session data is sent. State written by the plugin (config, notification state, update-check state, usage history) stays under `~/.config/herdr/plugins/config/usagebar/` and `~/.claude/herdr-usagebar/`.

## Limitations

- **Not a billing dashboard.** Local transcripts / rollouts / signals (and optional OpenCode web / Grok.com credits) can differ from official consoles (other machines, server-side windows).
- **Herdr core config is not rewritten on install.** Use `usagebar.setup` / `usagebar.enable-toast` or edit by hand.
- **macOS / Linux** only.

## Contributing

Bug fixes and documentation improvements are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) before starting a larger change.

## License

[MIT](LICENSE)
