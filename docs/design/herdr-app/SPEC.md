# Agent Usage in herdr-app: design spec

This is the design for running herdr-agent-usage (the `usagebar` plugin) inside
herdr-app, the native herdr; `app/` implements it. The plugin's "Agent Usage" limits pane
becomes a panel in herdr-app's right panel column, beside Files and Changes.

The PNGs below are design references with synthetic data, not pixel-exact
captures of the current mod. The tables and API contract describe `app/`.

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
  - The vendored `agent-usage-tabs` mod reads `shared("workspace.panels")`
    (default `["files","changes"]`) for the stock buttons and always appends
    Usage for this layout. No edit to the shipped `tabs` mod is required.
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

The panel consumes the versioned host contract, not the legacy debug JSON:

```sh
usagebar collect --format v1 [--all] --panes '<open-pane array>'
```

The report is documented in [collect-json-v1.md](../../collect-json-v1.md).
Go computes percentages, bands, reset wording and run-out text; the mod does
not derive quota semantics from raw `ProviderLimits` or parse note text for
status. `--quotas-only` is not used by the panel.

| Element | Example | Source field / module |
|---|---|---|
| Provider heading | Claude icon + **Claude** · `2 accounts` | `family` and `groupLabel` for grouped accounts; otherwise provider name + `planType`. Duplicate `sameAccountAs` entries fold into their retained owner |
| Account line | **personal** · Max ……… `alex@personal.example` | `profileLabel`, `planType`, and `identity` (email, else home path). `account` is an opaque proven join key, not display text |
| Window row | `5h ▬▬▬▬▬▬░░░ 62% left  2h 14m` | `windows[]`: `tag`, `displayPercent`, `barFill`, `resetIn`; `limitPercent` selects `left` or `used` |
| Bar colour | green / yellow / red | `windows[].band`, computed by Go: ≥50% left high, ≥20% mid, <20% low. Colour still follows remaining in used mode |
| Run-out warning | `⚠ empty in ~38m` | `windows[].runOut.text`, already worded by Go |
| Pane share | `5h share  api 71% · web 18% · closed / other 11%` | `paneActivity`; hidden by default, enabled with `"showPaneShare": true` in `local.json` |
| Spend estimate | `est. spent 5h $0.00/$12 · week $1.20/$30 · month $40.80/$60` | Provider `note`, e.g. the OpenCode Go local estimate |
| Freshness and source | `statusLine cache · updated 1m ago` | `source` and `fetchedAtMs` |
| Stale | window rows at 45% opacity, yellow freshness text | `status = "stale"` and `staleMinutes`, not note-text matching |
| Needs action | a callout: token expired — `run grok login` | `status = "needs-login"`, with `note`; backtick-delimited commands render as code. Other failure notes remain unboxed |
| Empty | "No agent panes open" | Report `emptyMessage`, or a local loading/error title before a report exists |
| Footer | `Updated 21:22 · every 15s` + **Refresh r** | The visible refresh cadence and focused `on_key_down` handler |

**Display scope:**

- Per-pane context meters (`$context`, `$cache_*`) stay in the sidebar, where herdr shows them
  per pane.
- The prompt-cache low-performance warning (`⚠ low cache performance: Task A 35.0%`)
  **is rendered** below the list when `lowCachePanes` is non-empty, in both full
  and compact views.
- Pay-as-you-go `apiUsage` blocks are present in the report but do not have a
  rendered section in this panel.

### Sizes (logical px)

| Part | Value |
|---|---|
| Provider heading | h34, gap 9, icon 16, 13 semibold; a 1px `border` separator between providers |
| Account line | h26, indent 39 (group padding 14 + account padding 25), 12.5; name medium; plan `muted`; hint 11 `dim`, right-aligned, truncated at 240 |
| Window row | h22, indent 39, gap 8: tag 24 (mono 11.5) · bar (flex, h6, radius 3, track = band colour at 15%) · percent 62 (12, band colour) · reset 54 (11.5 `dim`) |
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
  - `icon`: optional. Background polling does not collect a panel report just
    to resolve an icon; without one, the shipped toast uses its level marker.
  - `level`: `"warning"` at 50 / 20, `"error"` at 10 / 5.
  - `action`: `{ name: "agent-usage-tabs.open-usage" }`. The tab bar handles
    the command by selecting Usage and showing the panel. `local.json`
    `openCommand` can override it for a custom tab bar.
- **Account in the title** (`Session limit · work`, `Codex limit · personal`), so two Claude
  accounts do not produce identical toasts.
- **Delivery owner:** the always-visible tab bar polls `usagebar notify --json`
  at initialization and every 15s, independently of panel creation or collect
  success. The panel only collects/renders. Due toasts are recorded once by Go.
  The installer seeds `[notify] delivery = "external"` so herdr CLI delivery
  and the Claude statusLine bridge stay silent. See README for the shared
  statusLine policy when using both herdr TUI and herdr-app.

## 4. How the mod gets the data (herdr-app APIs)

| Need | herdr-app API |
|---|---|
| Snapshot | `exec("/usr/bin/env", ["HERDR_PLUGIN_CONFIG_DIR=…", binary, "collect", "--format", "v1", …])`, with `--all` for All and `--panes` from the app. Parse `{code, stdout, stderr}`; refresh every 15s visible, 60s hidden |
| Faster updates | Report `watchFiles` names exact Claude cache files; `watchFolder` on each parent is followed by a content comparison, ignoring history/notify writes. `watchDirs` names input-only Codex sessions trees. Hidden watch events defer to the normal 60s cadence |
| Open-panes filter | `openPanes(agents(), panes())` supplies a JSON array through `--panes`; no herdr CLI query is needed. A filter change queues the latest request and discards stale completions |
| Toasts | Always-visible tabs mod calls `notify({...})` (§3), not the hidden panel |
| Panel and button | Vendored tabs mod + layout `panel-body` stack, `layout.select` / `layout.show` / `layout.hide` |
| Refresh key | Panel `on_key_down` handles `r` while focused, refreshes and stops propagation; no `keys.json` binding |

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
7. **Fresh-data inputs:** `watchFiles` names exact Claude statusLine cache files;
   `watchDirs` names Codex rollout trees. Plugin output files never trigger
   another collect merely because their parent folder changed.

## 6. Decisions taken (change freely)

- **The button says "Usage".** It reads like Files and Changes and matches "Agent Usage". The
  panel title keeps the plugin's name, "Agent Usage".
- **Percent is "% left".** That's the plugin's default; `limit_percent = "used"` would flip the
  word and the fill as in the TUI.
- **Accounts stay grouped under one heading**, as the TUI already groups profiles.
- **The default filter is Open panes**, as the TUI pane's default; All is one click.
- **The design adds freshness** ("updated 1m ago" from `fetchedAtMs`) to every account.
