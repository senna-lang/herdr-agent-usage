# Agent Usage in herdr-app: design spec

This is the design for running herdr-agent-usage (the `usagebar` plugin) inside
herdr-app, the native herdr; `app/` implements it. The plugin's "Agent Usage" limits pane
becomes a panel in herdr-app's right panel column, beside Files and Changes.

Design file: Claude Design project "herdr-agent-usage · herdr-app", file `Agent Usage
herdr-app.dc.html`.

| PNG | What it shows |
|---|---|
| `a-window-usage-panel.png` | The stock herdr-app window (1.2 zoom, as in the reference screenshot) with the **Usage** tab-bar button active and the panel open on the right |
| `b-panel-2x-all-states.png` | The panel alone at 2x, with All selected: grouped accounts, a run-out warning, pane share, stale data, a spend estimate, an expired token, and no login |

Everything shown comes from data the plugin already collects. Nothing is invented: there are
no model-specific windows, because the repo has none.

## 1. Where it lives

- **Tab-bar button.** A **Usage** button goes next to Files and Changes. Its look is copied from
  the shipped `tabs` mod's `panelButton`: h26, px8, gap 5, radius 6, text 12, `selected` bg
  when active. The icon is 14px and ships with the mod; the design uses three small bars.
  - herdr-app decides which panel buttons show through `shared("workspace.panels")`, which
    defaults to `["files","changes"]`.
  - So the mod needs its id added there: `["files","changes","usage"]`.
  - It also needs the tabs mod to learn the button, or a small patch to `tabs`. Today the
    buttons are hard-coded in `tabs`.
- **Panel.** It's a third child of the layout's `panel-body` stack, next to `files` and `changes`:
  ```json
  { "id": "usage", "type": "block", "content": { "mod": "agent-usage" } }
  ```
  - The panel column is 368px (min 280, max 900), the layout's.
  - The button runs `layout.select {stack: "panel-body", child: "usage"}` and `layout.show
    {id: "panel"}`; a second click hides it, exactly as Files and Changes do.
- **Panel card:** the same shape as the shipped Changes panel. Rounded 10, 1px `border`, bg
  `background`, text 13.
  - Header: h42, px14, the title **Agent Usage** in semibold, the count chip (`N accounts`,
    the Changes "N files" chip style), and a segmented control.
  - The segmented control is **Open panes | All**. It is the plugin's existing filter:
    `usagebar limits` versus `usagebar limits --all`.

## 2. Panel content: what each element shows and where it comes from

The data source is the plugin's existing JSON command:

```
usagebar collect [--all] [--quotas-only]
→ { "providers": [ProviderLimits…], "apiUsage": [APIProviderUsage…] }
```

`ProviderLimits` is defined in `internal/limitscore/types.go`.

| Element | Example | Source field / module |
|---|---|---|
| Provider heading | Claude icon + **Claude** · `2 accounts` | `GroupLabel` when 2+ entries share it (Claude, Codex, Grok, OpenCode profiles: `internal/limits/claudeprofiles_io.go`). Otherwise `Label` + `PlanType` (`OpenCode · Go`) |
| Account line | **personal** · Max ……… `alex@personal.example` | Profile `label`, `PlanType` (from `internal/planlabels`: Max / Max 5x / Max 20x / Pro / Team / Plus / Go / SuperGrok …), and `AccountLabel`. For Claude, `AccountLabel` is the login email from `.claude.json`. Codex has no email, so the hint is the profile's `codex_home` |
| Window row | `5h ▬▬▬▬▬▬░░░ 62% left  2h 14m` | `Primary` / `Secondary` / `Tertiary` `LimitWindow`. The tag comes from `WindowMinutes` via `minutesTag` (5h / 24h / 7d / 30d). Percent = `100 − UsedPercentage`; the word is `left`, or `used` with `[ui] limit_percent = "used"`. The reset time comes from `ResetsAt` via `formatResetIn` ("4d 6h", "2h 14m", "44m", "soon") |
| Bar colour | green / yellow / red | `bar.ToneForRemaining`: **≥50 % left high, ≥20 % mid, <20 % low** (the pane's bands; the sidebar `$limit` band uses 25). The colour follows remaining even in `used` mode |
| Run-out warning | `⚠ empty in ~38m` | `LimitWindow.RunOut` when `EmptyBeforeReset` (`internal/limits/runout.go`), worded as `runOutLine` words it |
| Pane share | `5h share  api 71% · web 18% · closed / other 11%` | `PaneActivity` (`ProviderPaneActivity.Panes[].Label/SharePercent`), worded as `paneActivityLine`. Not present with `--quotas-only` |
| Spend estimate | `est. spent 5h $0.00/$12 · week $1.20/$30 · month $40.80/$60` | OpenCode Go `Note` from the local-estimate path (`internal/limits/opencodego.go`) |
| Freshness and source | `statusLine cache · updated 1m ago` | `Source` (e.g. "claude statusLine cache", "codex rollout", "opencode.ai web") + `FetchedAtMs` |
| Stale | rows at 45% opacity, `stale ~42m ago · codex rollout` in yellow | `Note = "stale ~Nm ago"` (`claudecache.go`, `claudejson.go`, `codexlimits.go`) |
| Needs action | a callout: `token expired (…) — run grok login` | `Note` text with no windows, `Source` "none" or an auth source (`groklimits.go`, `kilolimits.go`, `claudecache.go`: "no ~/.claude.json utilization and no statusLine cache"). The command in backticks is drawn as code |
| Empty | "No agent panes open" | The plugin's `EmptyMessage` "(no agent panes open)" for the open-panes filter; "(no usage data yet)" otherwise |
| Footer | `Updated 21:22 · every 15s` + **Refresh r** | The pane's 15 s refresh and `r` key (`fittingFooter`) |

**Not included on purpose:**

- Per-pane context meters (`$context`, `$cache_*`) stay in the sidebar, where herdr shows them
  per pane.
- The prompt-cache low-performance warning (`⚠ low cache performance: Task A 35.0%`) can be
  appended at the bottom of the list, exactly as the TUI pane does. It isn't drawn here.
- Pay-as-you-go `apiUsage` blocks (24h / 7d / 30d tokens and $) would render as their own
  section with the same row grid and no bar. They aren't drawn, to keep the proposal about
  limits.

### Sizes (logical px)

| Part | Value |
|---|---|
| Provider heading | h34, gap 9, icon 16, 13 semibold; a 1px `border` separator between providers |
| Account line | h26, indent 39 (under the heading text), 12.5; name 500; plan `muted`; hint 11 `dim`, right-aligned, truncated at 150 |
| Window row | h22, indent 39: tag 24 (mono 11.5) · bar (flex, h6, radius 3, track = band colour at 15 %) · percent 62 (12, band colour, tabular) · reset 54 (11.5 `dim`) |
| Warning | 11.5, band-low colour, aligned under the bar |
| Meta / notes | 11–11.5 `dim`; stale meta in band-mid colour |
| Callout | radius 7, bg `#ffffff08`, 1px `border`, 12 `muted`, command in mono on `raised` |
| Footer | h40, border-top, 11.5 `dim`, button h26 radius 7 on `raised` |

Colours use `theme()`: `background`, `border`, `raised`, `foreground`, `muted`, `dim`. The bands
map to `ansi2` (high), `ansi3` (mid) and `ansi1` (low), the same names the shipped Changes panel
uses for + and −. They follow light and dark themes.

### Compact view

- A header toggle (**Compact | Full**) switches to the plugin's existing compact tier
  (`compactLine`): one line per account, with the icon, `Provider account`, and each window as
  `tag ▬ N%`, plus `⚠` when a run-out warning exists.
- Useful when the panel is short or the user keeps it open beside a narrow terminal.

## 3. Toasts

- Same text as the plugin today:
  - Claude, from the statusLine: title `Session limit` / `Weekly limit`
  - other providers: `<Provider> limit`
  - body `N% remaining · resets in 1h 12m` (`internal/ratelimit/format.go`)
- Same thresholds: `[notify] remaining_thresholds`, default 50 / 20 / 10 / 5.
- In herdr-app, post them with:
  ```js
  notify({ title, body, level, icon, action })
  ```
  - `icon`: the harness icon from herdr-app's own program table (`icons/claude.svg`, …).
    The shipped `toast` mod draws it instead of the level dot.
  - `level`: `"warning"` at 50 / 20, `"error"` at 10 / 5.
  - `action`: `{ name: "layout.select", args: { stack: "panel-body", child: "usage" } }` (plus
    `layout.show {id: "panel"}`), so a click opens the panel.
- **Account in the title** (`Session limit · work`, `Codex limit · personal`), so two Claude
  accounts do not produce identical toasts.
- **No double toasts:** `usagebar notify --json` returns the toasts due now and records them as
  shown; the mod turns them into `notify()` calls. The installer seeds `[notify] delivery =
  "external"`, which keeps the herdr CLI delivery and the Claude statusLine bridge silent.

## 4. How the mod gets the data (herdr-app APIs)

| Need | herdr-app API |
|---|---|
| Snapshot | `exec("usagebar", ["collect", "--all"])`, or without `--all` for the open-panes filter. Parse stdout as JSON every 15 s while the panel is visible, and every 60 s when hidden (the plugin's own cadence). `exec` returns `{code, stdout, stderr}` |
| Faster updates | `watchFolder(<profile config_dir>)` and the Codex `sessions` dir, re-running `collect` when the statusLine cache or a rollout changes. `readTextFile` can read the plugin's limits cache files directly, but `collect` already merges every source, so prefer it |
| Open-panes filter | The plugin learns open panes from herdr itself. In herdr-app the mod can pass nothing and let `usagebar` ask, or later pass `agents()` / `terminals()` |
| Toasts | `notify({...})` (§3) |
| Panel and button | layout `panel-body` stack + `shared("workspace.panels")`, `layout.select` / `layout.show` / `layout.hide` |
| Refresh key | `keys.json` → `"r": "agent-usage:refresh"` while the panel is focused, received via `nextCommand()` |

## 5. What the Go side adds

All of it is built; the contract is in [docs/collect-json-v1.md](../../collect-json-v1.md).

1. **A stable JSON contract:** `usagebar collect --format v1` names fields in camelCase,
   carries `"version": 1`, and precomputes what the text panel derives (window tag, remaining
   percent, colour band, reset and run-out wording). Plain `collect` keeps its legacy shape.
2. **Profile label and identity as fields:** `profileId`, `profileLabel` and `identity`
   (email or home path), next to the grouped `groupLabel` / `accountLabel`, plus `family`,
   `account`, `sameAccountAs` and `home`.
3. **Structured status:** `status` (`ok`, `stale`, `needs-login`, `no-data`, ...),
   `staleMinutes` and `action` (`grok login`), so the panel picks the dimmed look and the
   callout without parsing `Note`.
4. **Toast data for the app:** `usagebar notify --json` returns the pending threshold crossings
   (`providerId`, `profileLabel`, `window`, `bucket`, `resetsAt`, `level`, `title`, `body`).
   It honours `[notify] enabled` and the billing gate, and shares its state with herdr delivery.
5. **Caller-supplied panes:** `collect --panes <json|->` and `notify --json --panes` take the
   open-pane list from the caller, so the app does not depend on the herdr CLI being on PATH.
6. **Which providers show:** each entry carries `inUse` and `configured`; the panel hides
   providers with nothing on the machine unless `[ui] providers` names them.
7. **Fresh-data folders:** `watchDirs` lists the folders whose changes mean new readings, so
   the mod re-collects without polling faster.

## 6. Decisions taken (change freely)

- **The button says "Usage".** It reads like Files and Changes and matches "Agent Usage". The
  panel title keeps the plugin's name, "Agent Usage".
- **Percent is "% left".** That's the plugin's default; `limit_percent = "used"` would flip the
  word and the fill as in the TUI.
- **Accounts stay grouped under one heading**, as the TUI already groups profiles.
- **The default filter is Open panes**, as the TUI pane's default; All is one click.
- **The design adds freshness** ("updated 1m ago" from `FetchedAtMs`) to every account.
