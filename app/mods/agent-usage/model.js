// Pure logic of the Agent Usage panel: report data in, display data out.
// No herdr or gpui imports, so `node --test` can run it (model.test.js).
// main.js holds the view and the herdr calls.

// The command a toast runs to open this panel: the tab bar mod that hosts the
// Usage button. local.json `openCommand` overrides it for layouts whose tab bar
// is another clone (e.g. one combined with other mods).
export const OPEN_COMMAND = "agent-usage-tabs.open-usage";

// Provider families as usagebar names them; icons from herdr-app's program table.
export const FAMILIES = {
  claude: { name: "Claude", icon: "icons/claude.svg", tint: false },
  codex: { name: "Codex", icon: "icons/codex.svg", tint: false },
  opencode: { name: "OpenCode", icon: "icons/opencode.svg", tint: true },
  grok: { name: "Grok", icon: "icons/grok.svg", tint: true },
  agy: { name: "Antigravity", icon: "icons/antigravity.svg", tint: false },
  kilo: { name: "Kilo Code", icon: "mods/agent-usage/usage.svg", tint: true },
  devin: { name: "Devin", icon: "mods/agent-usage/usage.svg", tint: true },
};
// herdr-app program keys that differ from usagebar's agent ids.
export const AGENT_IDS = { antigravity: "agy", "cursor-agent": "cursor" };

// Bar colours by the report's colour band.
export const bandColors = (t) => ({ high: t.ansi2, mid: t.ansi3, low: t.ansi1 });

// A bar's fill as a width; the report's fraction is clamped to 0-100%.
export const fillWidth = (w) => `${Math.round(Math.max(0, Math.min(1, w.barFill)) * 1000) / 10}%`;

// The report's empty-list wording ("(no agent panes open)") as a title.
export const emptyTitle = (message) => message.replace(/^\((.*)\)$/, "$1").replace(/^./, (c) => c.toUpperCase());

export const clock = (ms) => new Date(ms).toTimeString().slice(0, 5);
export const tilde = (path, homeDir) => (path && homeDir && path.startsWith(homeDir) ? "~" + path.slice(homeDir.length) : path ?? "");
export const sourceText = (source, homeDir) => (!source || source === "none" ? "no source" : tilde(source, homeDir));

export function ago(ms, now) {
  const min = Math.max(0, Math.round((now - ms) / 60000));
  if (min < 1) return "just now";
  if (min < 60) return `${min}m ago`;
  const h = Math.floor(min / 60);
  return h < 48 ? `${h}h ago` : `${Math.floor(h / 24)}d ago`;
}

// The open agent panes, in the shape `--panes` takes. `agents` and `panes`
// are herdr-app's agents() and panes() lists.
export function openPanes(agents, panes) {
  const byPane = new Map(panes.map((p) => [p.pane, p]));
  return agents.map((a) => {
    const key = a.program?.key ?? a.app ?? "";
    // The pane share names a pane by its tab (or space and tab number when
    // the tab is unnamed); usagebar adds the short terminal address only to
    // tell two panes of one tab apart.
    const label = /^\d+$/.test(a.tab_name ?? "") ? `${a.space_name} ${a.tab_name}` : a.tab_name || a.space_name;
    return { paneId: a.address, agent: AGENT_IDS[key] ?? key, label, cwd: byPane.get(a.pane)?.cwd ?? null };
  }).filter((p) => p.agent);
}

// Entries shown as rows (a second dir signed into an account already
// listed folds into that row), grouped by family when 2+ share it.
export function groupAccounts(report) {
  // Providers with nothing on this machine (usagebar's inUse) never show.
  const providers = (report?.providers ?? []).filter((p) => p.inUse !== false);
  const alsoIn = new Map();
  for (const p of providers) {
    if (!p.sameAccountAs) continue;
    alsoIn.set(p.sameAccountAs, [...(alsoIn.get(p.sameAccountAs) ?? []), p.profileLabel || p.label]);
  }
  const groups = [];
  for (const p of providers.filter((p) => !p.sameAccountAs)) {
    const last = groups[groups.length - 1];
    const row = { ...p, alsoIn: alsoIn.get(p.providerId) ?? [] };
    if (last && last.family === p.family) last.members.push(row);
    else groups.push({ family: p.family, members: [row] });
  }
  return groups;
}

// A lone account is headed by its provider (a profile label such as
// "codex" is just the account's name and adds nothing on its own).
export function groupName(g) {
  const first = g.members[0];
  return g.members.length > 1 ? first.groupLabel || FAMILIES[g.family]?.name || first.label : FAMILIES[g.family]?.name ?? first.label;
}

export const accountTitle = (p) => p.profileLabel || p.accountLabel || p.label;

// The line right of an account: its login or home, and the dirs sharing it.
export function hint(p, homeDir) {
  const identity = p.family === "claude" || p.identity?.includes("@") ? p.identity : tilde(p.identity, homeDir);
  return [identity, ...p.alsoIn.map((label) => `also ${label}`)].filter(Boolean).join(" · ");
}

export const percentWord = (report) => (report?.limitPercent === "used" ? "used" : "left");

// The per-pane token split line, or null when there is nothing to show.
export function shareLine(p, enabled) {
  const share = enabled ? (p.paneActivity?.panes ?? []) : [];
  if (!share.length) return null;
  return { tag: `${p.paneActivity.tag} share`, text: share.map((s) => `${s.label} ${Math.round(s.sharePercent)}%`).join(" · ") };
}

// Nothing recorded yet is a normal state, not a problem: say when it fills in.
export function noDataText(p, homeDir) {
  const agent = FAMILIES[p.family]?.name ?? p.label;
  return p.home ? `No usage yet · updates when ${agent} runs in ${tilde(p.home, homeDir)}` : "No usage yet";
}

// The source / freshness line under an account.
export function metaText(p, now, homeDir) {
  const stale = p.status === "stale";
  const parts = stale
    ? [`stale ~${p.staleMinutes}m ago`, sourceText(p.source, homeDir)]
    : p.windows.length ? [sourceText(p.source, homeDir), `updated ${ago(p.fetchedAtMs, now)}`] : [sourceText(p.source, homeDir)];
  return parts.join(" · ");
}

// A note split into text and `code` parts: odd entries are code.
export const calloutParts = (note) => note.split("`");

// The one-line account name in the compact view.
export function compactName(p, g) {
  const family = FAMILIES[g.family]?.name ?? p.label;
  return g.members.length > 1 ? `${family} ${accountTitle(p)}` : p.planType ? `${p.label} ${p.planType}` : p.label;
}

// What the compact view says for an account with no window.
export const compactPlaceholder = (p) => (p.status === "needs-login" ? `run ${p.action}` : p.status === "no-data" ? "no usage yet" : "no data");

export const lowCacheText = (report) =>
  "⚠ low cache performance: " + (report?.lowCachePanes ?? []).map((p) => `${p.label} ${p.hitPercent.toFixed(1)}%`).join(", ");

// The title of the empty state.
export function emptyStateTitle(report, error, all) {
  if (!report) return error ? "No data" : "Loading…";
  if (report.emptyMessage) return emptyTitle(report.emptyMessage);
  return all ? "No usage data yet" : "No agent panes open";
}

export function footerText(updatedAt, busy, everyMs) {
  return updatedAt ? `Updated ${clock(updatedAt)} · every ${everyMs / 1000}s` : busy ? "Loading…" : "";
}

// A failed usagebar run as a one-line message.
export function errorText(message, binary, homeDir) {
  return String(message).includes("No such file")
    ? `usagebar not found at ${tilde(binary, homeDir)}: run scripts/herdr-app-install.sh`
    : String(message);
}

// The toasts of `notify --json` as herdr-app notify() arguments. `report` is
// the last collect report (it maps a provider to its family icon).
export function toastNotifications(toasts, report, openCommand) {
  const families = new Map((report?.providers ?? []).map((p) => [p.providerId, p.family]));
  return (toasts ?? []).map((toast) => ({
    title: toast.title,
    body: toast.body,
    level: toast.level,
    icon: FAMILIES[families.get(toast.providerId)]?.icon,
    action: { name: openCommand ?? OPEN_COMMAND },
  }));
}
