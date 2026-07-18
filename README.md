# Agent Usage

[![CI](https://github.com/senna-lang/herdr-agent-usage/actions/workflows/ci.yml/badge.svg)](https://github.com/senna-lang/herdr-agent-usage/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
![Go 1.25+](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)
![herdr 0.7+](https://img.shields.io/badge/herdr-0.7%2B-6E56CF)
![platforms: linux | macOS](https://img.shields.io/badge/platforms-linux%20%7C%20macOS-lightgrey)

Monitor context usage and provider rate limits for agents running in [Herdr](https://herdr.dev).

![Agent Usage pane showing provider limits and per-pane activity shares](docs/assets/agent-usage-pane.png)

- **Per-pane context meters** — every agent pane's sidebar label shows how much of its context window the session is using (`⛁ 13% (130k)` = 130k tokens, 13% of the window), updated after each completed turn.
- **Account rate-limit windows at a glance** — one live pane shows how much 5h / 7d / 30d allowance is left for Claude, Codex, OpenCode Go, and Grok, with reset countdowns and which open pane is burning it.
- **Low-allowance warnings** — optional toasts fire when a window drops below your thresholds (default 50 / 20 / 10 / 5 % left), before you hit the wall mid-task.

## Requirements

- **Herdr ≥ 0.7.0**
- **macOS or Linux**
- Agent integrations for reliable session matching (recommended):

```bash
herdr integration install codex
herdr integration install opencode
# Claude Code integration recommended when you use Claude panes
```

## Install

```bash
herdr plugin install senna-lang/herdr-agent-usage
# non-interactive shells (CI, coding agents) need --yes
```

Plugin install does **not** rewrite `~/.config/herdr/config.toml` (toast delivery, keybindings). Run setup after install:

```bash
herdr plugin action invoke usagebar.setup
# optional: append toast delivery if missing
herdr plugin action invoke usagebar.enable-toast
herdr server reload-config
```

`usagebar.setup` resolves the `usagebar` binary automatically on first run: it builds with the local Go toolchain (≥ 1.25) when available, and otherwise downloads a prebuilt binary from [GitHub Releases](https://github.com/senna-lang/herdr-agent-usage/releases) (macOS / Linux, arm64 / amd64). To build manually instead, run `make build` in the plugin root.

## Let an LLM set it up

Copy the prompt in [docs/LLM-SETUP.md](docs/LLM-SETUP.md) into an LLM coding agent.
The agent can install the plugin and guide you through the remaining setup.

- **Toasts:** The agent must ask for your approval before enabling toast notifications.
- **Keybindings:** The recommended shortcuts are `ctrl+shift+u` to open the limits pane and `ctrl+shift+m` to refresh meters (single chords; no Herdr prefix). If either shortcut is already in use, the agent must ask which shortcut to use instead.

## Quick start

1. Install the plugin and run **setup** (above).
2. Open a workspace with at least one agent pane.
3. After an agent turn completes (or you focus the pane), the sidebar custom status shows context usage.
4. Open the limits pane:

```bash
herdr plugin action invoke usagebar.open-limits
```

5. Optional keybindings in **your** `~/.config/herdr/config.toml`:

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
| Refresh meters | `usagebar.refresh` | Recompute sidebar custom status for the target pane |
| Setup | `usagebar.setup` | Seed plugin config, show toast/key snippets, report Herdr toast status |
| Enable toast | `usagebar.enable-toast` | Append `[ui.toast]` only if missing (never overwrites) |
| Check for updates | `usagebar.check-updates` | Check GitHub Releases now and show the release/update instructions |

```bash
herdr plugin action list --plugin usagebar
herdr plugin action invoke usagebar.setup
```

## What you get

| Surface | What it shows |
| --- | --- |
| **Sidebar custom status** | Per-pane context usage: `⛁ 13% (130k)` when the window size is known, or the token count alone |
| **Agent Usage pane** | Provider plan, usage windows (5h / 7d / 30d), remaining % bars, reset countdown, open-pane token share |
| **Toasts** (optional) | Remaining-limit warnings at configured thresholds (default 50 / 20 / 10 / 5 % left) |

### Supported agents

| Agent | Sidebar context | Limits pane | Notes |
| --- | --- | --- | --- |
| Claude Code | Yes | Yes | Rate windows from `~/.claude.json` / statusLine cache |
| Codex | Yes | Yes | Context + rate windows from local rollouts |
| OpenCode Go | Yes | Yes | Prefer official web usage when `OPENCODE_GO_COOKIE` is set; else local SQLite |
| Grok | Yes | Yes | Context from `signals.json`; credits from grok.com when auth is present |

Percentages in the limits pane are **remaining** (`% left`). Higher is safer.

## Agent Usage pane

- Auto-refreshes every **15s**. Press **`r`** to refresh, **`q`** to quit.
- OpenCode Go may show three windows (**5h / 7d / 30d**). Other providers show whichever usage windows their data sources make available.
- Open pane **token share** is local activity share within the shortest window (including a **closed / other** bucket for usage outside open panes). It is not account quota attribution.
- Sidebar context meters update after the agent has **settled** (not while `working`), so the label matches the last completed turn. If the session cannot be resolved, the custom status is cleared rather than showing another session’s numbers.

```bash
herdr plugin action invoke usagebar.open-limits
```

## Configuration

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

### OpenCode Go official usage (optional)

Set the Cookie request header if you want web-backed numbers from the OpenCode console:

```bash
export OPENCODE_GO_COOKIE='auth=…'
```

Without it, usage is estimated from local `~/.local/share/opencode/opencode.db` (5h rolling, UTC week, calendar month).

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

## Data handling

Everything is computed from files that the agents already keep on your machine:

| Provider | Local sources read |
| --- | --- |
| Claude Code | `~/.claude.json`, statusLine cache under `~/.claude/herdr-usagebar/` |
| Codex | rollout files under `~/.codex/sessions/` |
| OpenCode Go | `~/.local/share/opencode/opencode.db` |
| Grok | `~/.grok/sessions/**/signals.json`, `~/.grok/auth.json` (credentials for the credits fetch) |

Network requests happen in the following cases:

- `opencode.ai` — only when you set `OPENCODE_GO_COOKIE`
- `grok.com` — only when `~/.grok/auth.json` exists (you ran `grok login`)
- `api.github.com` — on the first pane focus and then at most once every 24 hours, to check this plugin's latest public release. The request has no credentials and sends no usage or session data.

No telemetry, no analytics, or usage/session data is sent. State written by the plugin (config, notification state, update-check state, usage history) stays under `~/.config/herdr/plugins/config/usagebar/` and `~/.claude/herdr-usagebar/`.

## Limitations

- **Not a billing dashboard.** Local transcripts / rollouts / signals (and optional OpenCode web / Grok.com credits) can differ from official consoles (other machines, server-side windows).
- **Herdr core config is not rewritten on install.** Use `usagebar.setup` / `usagebar.enable-toast` or edit by hand.
- **macOS / Linux** only.

## License

[MIT](LICENSE)
