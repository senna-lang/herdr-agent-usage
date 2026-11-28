// Run with: node --test "app/**/*.test.js"  (Node 22.12+). Fake data only.
import { test } from "node:test";
import assert from "node:assert/strict";
import {
  FAMILIES, OPEN_COMMAND, bandColors, fillWidth, emptyTitle, clock, tilde, sourceText, ago, openPanes,
  groupAccounts, groupName, accountTitle, hint, percentWord, shareLine, noDataText, metaText, calloutParts,
  compactName, compactPlaceholder, lowCacheText, emptyStateTitle, footerText, errorText, toastNotifications,
} from "./model.js";

const HOME = "/home/user-1";
const row = (over = {}) => ({
  providerId: "claude", family: "claude", label: "Claude", profileLabel: "", accountLabel: "", groupLabel: "",
  identity: "", home: "", planType: null, status: "ok", windows: [], source: "claude statusLine cache",
  fetchedAtMs: 0, ...over,
});

test("bandColors maps the report bands to theme colours", () => {
  const t = { ansi1: "red", ansi2: "green", ansi3: "yellow" };
  assert.deepEqual(bandColors(t), { high: "green", mid: "yellow", low: "red" });
});

test("fillWidth clamps to 0-100% with one decimal", () => {
  assert.equal(fillWidth({ barFill: 0.4567 }), "45.7%");
  assert.equal(fillWidth({ barFill: 1.7 }), "100%");
  assert.equal(fillWidth({ barFill: -0.2 }), "0%");
});

test("emptyTitle strips the parentheses and capitalises", () => {
  assert.equal(emptyTitle("(no agent panes open)"), "No agent panes open");
  assert.equal(emptyTitle("already plain"), "Already plain");
});

test("tilde and sourceText shorten the home dir", () => {
  assert.equal(tilde(`${HOME}/.claude`, HOME), "~/.claude");
  assert.equal(tilde("/etc/x", HOME), "/etc/x");
  assert.equal(tilde(undefined, HOME), "");
  assert.equal(tilde("/a", ""), "/a");
  assert.equal(sourceText("none", HOME), "no source");
  assert.equal(sourceText("", HOME), "no source");
  assert.equal(sourceText(`${HOME}/x.json`, HOME), "~/x.json");
});

test("ago words", () => {
  const now = 10_000_000_000;
  assert.equal(ago(now - 20_000, now), "just now");
  assert.equal(ago(now - 5 * 60_000, now), "5m ago");
  assert.equal(ago(now - 3 * 3_600_000, now), "3h ago");
  assert.equal(ago(now - 30 * 3_600_000, now), "30h ago");
  assert.equal(ago(now - 50 * 3_600_000, now), "2d ago");
  assert.equal(ago(now + 60_000, now), "just now");
});

test("clock is HH:MM", () => {
  assert.match(clock(0), /^\d\d:\d\d$/);
});

test("openPanes maps herdr agents to --panes entries", () => {
  const agents = [
    { address: "a1", pane: 1, program: { key: "antigravity" }, tab_name: "api", space_name: "Project Alpha" },
    { address: "a2", pane: 2, program: { key: "claude" }, tab_name: "3", space_name: "Project Alpha" },
    { address: "a3", pane: 3, app: "codex", tab_name: "", space_name: "Project Beta" },
    { address: "a4", pane: 4, tab_name: "x", space_name: "y" },
  ];
  const panes = [{ pane: 2, cwd: "/work/alpha" }];
  assert.deepEqual(openPanes(agents, panes), [
    { paneId: "a1", agent: "agy", label: "api", cwd: null },
    { paneId: "a2", agent: "claude", label: "Project Alpha 3", cwd: "/work/alpha" },
    { paneId: "a3", agent: "codex", label: "Project Beta", cwd: null },
  ]);
});

test("groupAccounts hides unused entries, folds shared accounts and groups by family", () => {
  const report = {
    providers: [
      row({ providerId: "claude", profileLabel: "personal" }),
      row({ providerId: "claude-2", profileLabel: "second", sameAccountAs: "claude" }),
      row({ providerId: "claude-3", profileLabel: "work" }),
      row({ providerId: "codex", family: "codex", label: "Codex" }),
      row({ providerId: "grok", family: "grok", label: "Grok", inUse: false }),
    ],
  };
  const groups = groupAccounts(report);
  assert.deepEqual(groups.map((g) => [g.family, g.members.map((m) => m.providerId)]), [
    ["claude", ["claude", "claude-3"]],
    ["codex", ["codex"]],
  ]);
  assert.deepEqual(groups[0].members[0].alsoIn, ["second"]);
  assert.deepEqual(groups[0].members[1].alsoIn, []);
  assert.deepEqual(groupAccounts(null), []);
});

test("groupName heads a lone account by its provider and a group by its label", () => {
  const lone = { family: "codex", members: [row({ family: "codex", label: "personal" })] };
  assert.equal(groupName(lone), "Codex");
  const grouped = { family: "codex", members: [row({ family: "codex", groupLabel: "Codex" }), row({ family: "codex" })] };
  assert.equal(groupName(grouped), "Codex");
  const unlabeled = { family: "claude", members: [row({ groupLabel: "" }), row()] };
  assert.equal(groupName(unlabeled), "Claude");
  const unknown = { family: "zzz", members: [row({ family: "zzz", label: "Zed" })] };
  assert.equal(groupName(unknown), "Zed");
});

test("accountTitle prefers the profile, then account, then label", () => {
  assert.equal(accountTitle(row({ profileLabel: "a", accountLabel: "b" })), "a");
  assert.equal(accountTitle(row({ accountLabel: "b" })), "b");
  assert.equal(accountTitle(row()), "Claude");
});

test("hint shows the login or a shortened home plus shared dirs", () => {
  assert.equal(hint(row({ identity: "user-1@example", alsoIn: ["second"] }), HOME), "user-1@example · also second");
  assert.equal(hint(row({ family: "codex", identity: `${HOME}/.codex`, alsoIn: [] }), HOME), "~/.codex");
  assert.equal(hint(row({ family: "claude", identity: `${HOME}/.claude`, alsoIn: [] }), HOME), `${HOME}/.claude`);
  assert.equal(hint(row({ family: "codex", identity: "", alsoIn: [] }), HOME), "");
});

test("percentWord follows limitPercent", () => {
  assert.equal(percentWord({ limitPercent: "used" }), "used");
  assert.equal(percentWord({ limitPercent: "remaining" }), "left");
  assert.equal(percentWord(null), "left");
});

test("shareLine is null unless enabled and non-empty", () => {
  const p = row({ paneActivity: { tag: "5h", panes: [{ label: "api", sharePercent: 70.6 }, { label: "web", sharePercent: 29.4 }] } });
  assert.equal(shareLine(p, false), null);
  assert.equal(shareLine(row(), true), null);
  assert.deepEqual(shareLine(p, true), { tag: "5h share", text: "api 71% · web 29%" });
});

test("noDataText says where the data will appear", () => {
  assert.equal(noDataText(row({ family: "codex", home: `${HOME}/.codex` }), HOME), "No usage yet · updates when Codex runs in ~/.codex");
  assert.equal(noDataText(row({ home: "" }), HOME), "No usage yet");
});

test("metaText covers stale, fresh and window-less entries", () => {
  const now = 10_000_000_000;
  assert.equal(metaText(row({ status: "stale", staleMinutes: 42, source: "codex rollout" }), now, HOME), "stale ~42m ago · codex rollout");
  assert.equal(metaText(row({ windows: [{}], fetchedAtMs: now - 120_000 }), now, HOME), "claude statusLine cache · updated 2m ago");
  assert.equal(metaText(row({ source: "none" }), now, HOME), "no source");
});

test("calloutParts alternates text and code", () => {
  assert.deepEqual(calloutParts("token expired, run `grok login` now"), ["token expired, run ", "grok login", " now"]);
});

test("compactName and compactPlaceholder", () => {
  const lone = { family: "claude", members: [row({ label: "Claude", planType: "Max" })] };
  assert.equal(compactName(lone.members[0], lone), "Claude Max");
  const noPlan = { family: "claude", members: [row()] };
  assert.equal(compactName(noPlan.members[0], noPlan), "Claude");
  const two = { family: "claude", members: [row({ profileLabel: "work" }), row()] };
  assert.equal(compactName(two.members[0], two), "Claude work");
  assert.equal(compactPlaceholder(row({ status: "needs-login", action: "grok login" })), "run grok login");
  assert.equal(compactPlaceholder(row({ status: "no-data" })), "no usage yet");
  assert.equal(compactPlaceholder(row({ status: "unavailable" })), "no data");
});

test("lowCacheText lists panes with one decimal", () => {
  assert.equal(lowCacheText({ lowCachePanes: [{ label: "Task A", hitPercent: 35 }, { label: "Task B", hitPercent: 12.34 }] }),
    "⚠ low cache performance: Task A 35.0%, Task B 12.3%");
});

test("emptyStateTitle prefers the report's wording", () => {
  assert.equal(emptyStateTitle(null, null, false), "Loading…");
  assert.equal(emptyStateTitle(null, "boom", false), "No data");
  assert.equal(emptyStateTitle({ emptyMessage: "(no usage data yet)" }, null, false), "No usage data yet");
  assert.equal(emptyStateTitle({}, null, false), "No agent panes open");
  assert.equal(emptyStateTitle({}, null, true), "No usage data yet");
});

test("footerText", () => {
  assert.match(footerText(1, false, 15000), /^Updated \d\d:\d\d · every 15s$/);
  assert.equal(footerText(null, true, 15000), "Loading…");
  assert.equal(footerText(null, false, 15000), "");
});

test("errorText explains a missing binary", () => {
  assert.equal(errorText("spawn /x: No such file or directory", `${HOME}/.local/bin/usagebar`, HOME),
    "usagebar not found at ~/.local/bin/usagebar: run scripts/herdr-app-install.sh");
  assert.equal(errorText(new Error("bad"), "/x", HOME), "Error: bad");
});

test("toastNotifications maps toasts to notify arguments with the family icon", () => {
  const report = { providers: [{ providerId: "claude-2", family: "claude" }, { providerId: "odd", family: "zzz" }] };
  const toasts = [
    { providerId: "claude-2", title: "Session limit · work", body: "20% remaining", level: "warning" },
    { providerId: "odd", title: "t", body: "b", level: "error" },
  ];
  const out = toastNotifications(toasts, report, undefined);
  assert.deepEqual(out[0], { title: "Session limit · work", body: "20% remaining", level: "warning", icon: FAMILIES.claude.icon, action: { name: OPEN_COMMAND } });
  assert.equal(out[1].icon, undefined);
  assert.equal(toastNotifications(toasts, report, "custom.open")[0].action.name, "custom.open");
  assert.deepEqual(toastNotifications(undefined, report), []);
});
