# Agent Usage — LLM setup prompt

Copy everything below the line into an AI agent.
That agent should install and configure the **Agent Usage** Herdr plugin for you.

---

You are setting up the **Agent Usage** Herdr plugin (`usagebar`) for the user.
Follow every step in order. Do not skip confirmation gates.

## Hard rules (do not violate)

1. **Notifications / toasts — never decide alone.**  
   Do **not** enable Herdr toast delivery, run `usagebar.enable-toast`, or append
   `[ui.toast]` unless the user has **explicitly** answered yes after you asked.  
   If they decline or skip, leave toast config untouched and continue.

2. **Keybindings — check for conflicts first.**  
   Recommended bindings (from the plugin README) — single chords, **no**
   Herdr `prefix` mode:
   - `ctrl+shift+u` → `usagebar.open-limits`
   - `ctrl+shift+m` → `usagebar.refresh`  

   Before writing any binding, inspect `~/.config/herdr/config.toml` (or
   `$HERDR_CONFIG` / `$XDG_CONFIG_HOME/herdr/config.toml`) for existing
   `[[keys.command]]` entries.  
   - If **either** recommended key is already used by another command, **stop
     and ask** the user which key(s) to use for the free action(s). Do not
     overwrite or steal keys.  
   - If a recommended binding for Agent Usage already exists with the correct
     `command`, leave it as-is.  
   - If older `prefix+…` bindings for the same `usagebar.*` commands exist,
     replace them with the recommended chords (or the user’s chosen keys).  
   - Only append/update bindings the user has agreed to (defaults when free,
     or the keys they chose when recommended ones were taken).

3. **Never overwrite existing user config.**  
   - Toast: only append when `[ui.toast]` is **missing** (prefer
     `herdr plugin action invoke usagebar.enable-toast`, which is safe).  
   - Keybindings: append new `[[keys.command]]` blocks; do not rewrite
     unrelated sections.  
   - Sidebar: never append a second `[ui.sidebar.agents]` table. If it already
     exists, merge `$limit` and `$context` into its existing `rows` while
     preserving unrelated rows and tokens. Apply the same merge to relevant
     `rows_by_agent` overrides because those replace the default rows.
   - Do not delete or rewrite the whole `config.toml`.

4. Prefer official CLI actions over hand-editing when possible.

## Goal

End state:

- Plugin `usagebar` installed and enabled  
- Plugin config seeded (`usagebar.setup`)  
- Sidebar shows provider limit above the unchanged context meter
- Optional: toast delivery configured **only if the user said yes**  
- Optional: keybindings for open-limits / refresh **with conflict handling**  
- `herdr server reload-config` after any Herdr config change  
- Short summary of what was done and what was left for the user  

## Steps

### 1. Prerequisites

- Confirm `herdr` is on `PATH` and works (`herdr --help` or `herdr plugin list`).  
- Herdr **≥ 0.7.4** is required.
- OS: macOS or Linux.  
- **Go toolchain ≥ 1.25** (`go version`) recommended. `usagebar.setup`
  resolves the binary automatically on first run: it builds with Go when
  available, else downloads a prebuilt binary from GitHub Releases. If
  neither Go nor a working download path exists, stop and ask the user
  to install Go.  
- Recommended (ask if missing, do not force):

```bash
herdr integration install codex
herdr integration install opencode
```

### 2. Install the plugin

First inspect the current state with `herdr plugin list`:

- If `usagebar` is already installed from `senna-lang/herdr-agent-usage`,
  skip reinstall and go to step 3.  
- **Plugin-id collision:** if a *different* plugin already claims the id
  `usagebar` (e.g. a locally linked dev tree or a self-made compatible
  plugin), installing would conflict. Tell the user what you found, get
  their OK, then remove the old one first:

```bash
herdr plugin unlink usagebar        # for a linked local plugin
# or: herdr plugin uninstall <owner>/<repo>[/subdir...]
```

Then install. `--yes` is **required** whenever stdin is not interactive —
which is the case when an agent runs this command:

```bash
herdr plugin install senna-lang/herdr-agent-usage --yes
```

Verify:

```bash
herdr plugin list
```

Expect `usagebar` (Agent Usage) **enabled**.

### 3. Seed plugin config (builds the binary on first run)

```bash
herdr plugin action invoke usagebar.setup
```

This does two things:

- **Resolves `bin/usagebar`** automatically if it is missing: builds with
  the local Go toolchain, else downloads a prebuilt GitHub Release binary.
  No separate `make build` is needed in the normal flow.  
- Creates plugin config under
  `~/.config/herdr/plugins/config/usagebar/config.toml` when missing
  (`[notify]` thresholds, etc.). It does **not** by itself enable toast
  delivery unless the user later opts in.

Action invokes are asynchronous — the CLI returns before the action
finishes. Check the outcome via the plugin log:

```bash
herdr plugin log list --plugin usagebar --limit 5
```

If the log shows a binary-resolution failure (exit 127): install Go and run
`make build` from the plugin root (resolve the path via `herdr plugin
list`), or install + authenticate `gh` for the prebuilt download, then
re-invoke `usagebar.setup`.

### 4. Sidebar rows (required)

Read the active Herdr config first. The target Agent layout is:

```toml
[ui.sidebar.agents]
row_gap = 0
rows = [
  ["state_icon", "tab", "pane"],
  ["agent", "$limit"],
  ["$context"],
]
```

- If `[ui.sidebar.agents]` is absent, append the block above.
- If it exists, do not append another table. Preserve its existing layout,
  add `$limit` to the row containing `agent`, and add `$context` as the next
  row. Do not remove unrelated tokens or rows.
- If `[ui.sidebar.agents.rows_by_agent]` contains overrides for Claude,
  Codex, OpenCode, or Grok, merge `$limit` and `$context` into each relevant
  override too; an override replaces the default `rows`.

After changing the config:

```bash
herdr server reload-config
```

### 5. Notifications (mandatory user confirmation)

Ask the user clearly, e.g.:

> Enable Herdr toast notifications for rate-limit warnings  
> (remaining 50% / 20% / 10% / 5%)?  
> **Yes / No**

- **Yes** → only then:

```bash
herdr plugin action invoke usagebar.enable-toast
herdr server reload-config
```

  Confirm toast is present (setup output or inspect config for `[ui.toast]`).  
  If toast was already configured, report that and do not overwrite.

- **No** / skip → do nothing to toast settings. Say notifications will not
  appear until they enable toast later via `usagebar.enable-toast`.

### 6. Keybindings (conflict check, then apply)

Target commands:

| Purpose | `command` value | Recommended `key` | Mac |
| --- | --- | --- | --- |
| Open limits pane | `usagebar.open-limits` | `ctrl+shift+u` | Control+Shift+U |
| Refresh sidebar meters | `usagebar.refresh` | `ctrl+shift+m` | Control+Shift+M |

1. Read `~/.config/herdr/config.toml`.  
2. For each recommended key, see if another `[[keys.command]]` already uses it.  
3. If a conflict exists for a recommended key, **ask the user** which key to
   use for that action (examples: `ctrl+shift+a`, `alt+u`). Do not guess.  
4. If no conflict, you may use the recommended keys without re-asking for
   which keys—but still tell the user what you will add before writing.  
5. Append only missing bindings (or replace outdated `prefix+…` bindings for
   the same `usagebar.*` commands). Example shape:

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

6. After any keybinding change:

```bash
herdr server reload-config
```

If the user declines keybindings, skip this step.

### 7. Optional: Claude statusLine

Only if the user uses Claude Code and wants 5h/7d rate windows + Claude
toasts via statusLine, offer to help. Do not change Claude settings without
asking. The command should point at this plugin’s `bin/run-statusline.sh`
(resolve path from `herdr plugin list` / plugin root). Prefer chaining with
an existing statusLine rather than replacing it.

### 8. Smoke check

```bash
herdr plugin list
herdr plugin action list --plugin usagebar
herdr plugin action invoke usagebar.open-limits
```

After an agent turn completes on a supported pane (Claude / Codex / OpenCode /
Grok), verify with `herdr pane get <pane-id>` that both `tokens.context` and
`tokens.limit` are present. The sidebar should render limit above context.

### 9. Report back

Summarize in plain language:

- Installed / already present  
- Plugin config path  
- Sidebar rows: added / merged / already configured
- Toast: enabled / already present / skipped by user  
- Keys: which bindings were added, or that the user declined / chose alternates  
- Any remaining manual steps  

## Reference

- Plugin id: `usagebar`  
- Install: `herdr plugin install senna-lang/herdr-agent-usage --yes`  
- Docs: repository `README.md`  
- Safe toast enable: `herdr plugin action invoke usagebar.enable-toast`  
  (appends only when `[ui.toast]` is missing; never overwrites)
