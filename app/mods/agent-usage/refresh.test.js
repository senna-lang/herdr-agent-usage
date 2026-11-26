// Filter races and toast ownership (review blockers 1 and 6). Runs the real panel mod.
// node --experimental-vm-modules --test 'app/**/*.test.js'
import { test, before, beforeEach, afterEach } from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import {
  Host, loadDefault, flush, jsonResult, layoutWith, LOCAL_JSON, parentDir, VISIBLE_MS,
} from "../../test-support/host-harness.js";

const usageMain = fileURLToPath(new URL("./main.js", import.meta.url));
const host = new Host();
const STALE_DIR = "/var/stale-open";
const ALL_SESSIONS = "/var/fresh-sessions";
const ALL_CACHE = "/var/fresh-cache/claude-limits-latest.json";
const OPEN_MS = 111;
const ALL_MS = 222;

let Usage;

before(async () => {
  host.installGlobals();
  Usage = await loadDefault(usageMain, host);
});

beforeEach(() => {
  host.reset();
  host.installGlobals();
  host.files.set(LOCAL_JSON, JSON.stringify({ binary: "/opt/usagebar", configDir: "/opt/agent-usage" }));
});

afterEach(() => host.restoreGlobals());

function report(filter, extra = {}) {
  return {
    version: 1,
    generatedAtMs: extra.generatedAtMs ?? (filter === "all" ? ALL_MS : OPEN_MS),
    filter,
    limitPercent: "remaining",
    emptyMessage: filter === "all" ? "(no usage data yet)" : "(no agent panes open)",
    providers: [],
    apiUsage: [],
    lowCachePanes: [],
    watchDirs: extra.watchDirs ?? [],
    watchFiles: extra.watchFiles ?? [],
  };
}

async function startOpenCollect() {
  const view = new Usage();
  view.init({}, host.cx);
  await flush();
  const [collect] = host.pending("collect");
  assert.ok(collect, `init did not start an open-panes collect: ${host.debug()}`);
  assert.equal(collect.args.includes("--all"), false, "the in-flight collect must stay the original open-panes request");
  return { view, collect };
}

function publishedOpen(view) {
  return view.report?.filter === "open-panes" || view.report?.generatedAtMs === OPEN_MS;
}

test("a slow open-panes collect is not published after switching to All", async () => {
  const { view, collect } = await startOpenCollect();
  view.setFilter(true, host.cx);
  assert.equal(view.all, true);
  collect.settle(jsonResult(report("open-panes", { generatedAtMs: OPEN_MS, watchDirs: [STALE_DIR] })));
  await flush();
  assert.equal(publishedOpen(view), false, "the open-panes result must not flash after All is selected");
  assert.equal(view.error, null);
});

test("switching to All while collecting queues one All collect immediately", async () => {
  const { view, collect } = await startOpenCollect();
  const started = host.now;
  view.setFilter(true, host.cx);
  assert.equal(host.pending("collect").length, 1, "the in-flight collect must not be joined by a parallel one");
  collect.settle(jsonResult(report("open-panes", { generatedAtMs: OPEN_MS })));
  await flush();
  assert.equal(host.now, started, "the queued collect must not wait for the 15s timer");
  const follow = host.pending("collect");
  assert.equal(follow.length, 1, `All must be collected as soon as the open-panes collect finishes: ${host.debug()}`);
  assert.ok(follow[0].args.includes("--all"));
  assert.equal(publishedOpen(view), false);
  follow[0].settle(jsonResult(report("all", { generatedAtMs: ALL_MS })));
  await flush();
  assert.equal(view.report?.filter, "all");
  assert.equal(view.report?.generatedAtMs, ALL_MS);
});

test("a failed open-panes collect is not published after switching to All", async () => {
  const { view, collect } = await startOpenCollect();
  view.setFilter(true, host.cx);
  collect.settle({ code: 1, stdout: "", stderr: "collect failed" });
  await flush();
  assert.equal(view.error, null, "a stale failure must not replace the panel error");
  assert.equal(publishedOpen(view), false);
  const follow = host.pending("collect");
  assert.equal(follow.length, 1, `All must still be queued after the stale failure: ${host.debug()}`);
  assert.ok(follow[0].args.includes("--all"));
});

test("a stale open-panes completion does not register that report's watches", async () => {
  const { view, collect } = await startOpenCollect();
  view.setFilter(true, host.cx);
  collect.settle(jsonResult(report("open-panes", {
    generatedAtMs: OPEN_MS,
    watchDirs: [STALE_DIR],
    watchFiles: [`${STALE_DIR}/claude-limits-latest.json`],
  })));
  await flush();
  assert.equal(host.watched.has(STALE_DIR), false, "stale completion must not arm watches for the old filter");
});

test("the queued All collect publishes and watches only the All report", async () => {
  const { view, collect } = await startOpenCollect();
  view.setFilter(true, host.cx);
  collect.settle(jsonResult(report("open-panes", { generatedAtMs: OPEN_MS, watchDirs: [STALE_DIR] })));
  await flush();
  const [all] = host.pending("collect");
  assert.ok(all, `queued All collect missing: ${host.debug()}`);
  host.files.set(ALL_CACHE, "v1");
  all.settle(jsonResult(report("all", {
    generatedAtMs: ALL_MS,
    watchDirs: [ALL_SESSIONS],
    watchFiles: [ALL_CACHE],
  })));
  await flush();
  assert.equal(view.report?.filter, "all");
  assert.equal(host.watched.has(STALE_DIR), false);
  assert.equal(host.watched.has(ALL_SESSIONS), true, "All watchDirs must be armed from the matching report");
  assert.equal(host.watched.has(parentDir(ALL_CACHE)), true, "All watchFiles parents must be armed from the matching report");
});

test("rapid filter toggles coalesce to one collect for the latest filter", async () => {
  const { view, collect } = await startOpenCollect();
  view.setFilter(true, host.cx);
  view.setFilter(false, host.cx);
  view.setFilter(true, host.cx);
  assert.equal(host.pending("collect").length, 1, "toggles while busy must not start parallel collects");
  collect.settle(jsonResult(report("open-panes", { generatedAtMs: OPEN_MS })));
  await flush();
  assert.equal(publishedOpen(view), false);
  const follow = host.pending("collect");
  assert.equal(follow.length, 1, `three toggles must coalesce to one rerun: ${host.debug()}`);
  assert.ok(follow[0].args.includes("--all"), "the coalesced rerun must use the latest filter");
  view.setFilter(false, host.cx);
  follow[0].settle(jsonResult(report("all", { generatedAtMs: ALL_MS })));
  await flush();
  assert.notEqual(view.report?.filter, "all", "the in-flight All result is stale once Open panes is selected again");
  const again = host.pending("collect");
  assert.equal(again.length, 1, "a toggle during the queued collect must queue exactly one more rerun");
  assert.equal(again[0].args.includes("--all"), false);
});

test("rapid toggles back to open panes coalesce to one open collect", async () => {
  const { view, collect } = await startOpenCollect();
  view.setFilter(true, host.cx);
  view.setFilter(false, host.cx);
  collect.settle(jsonResult(report("open-panes", { generatedAtMs: OPEN_MS })));
  await flush();
  assert.equal(publishedOpen(view), false, "toggling away and back must not publish the pre-toggle collect");
  const follow = host.pending("collect");
  assert.equal(follow.length, 1, `latest open-panes selection must still rerun: ${host.debug()}`);
  assert.equal(follow[0].args.includes("--all"), false);
});

test("setFilter notifies the UI while a refresh is in flight", async () => {
  const { view } = await startOpenCollect();
  const before = host.uiNotifies;
  view.setFilter(true, host.cx);
  assert.ok(host.uiNotifies > before, "setFilter must repaint All even though the open-panes collect is still running");
});

test("a slow same-filter collect that spans two 15s ticks still publishes", async () => {
  host.layout = layoutWith({ panelHidden: false, active: "usage" });
  const view = new Usage();
  view.init({}, host.cx);
  await flush();
  const first = host.pending("collect")[0];
  assert.ok(first, `init collect missing: ${host.debug()}`);
  await host.advance(VISIBLE_MS);
  assert.equal(host.pending("collect").length, 1, "the visible timer must queue, not run beside the in-flight collect");
  first.settle(jsonResult(report("open-panes", { generatedAtMs: 1 })));
  await flush();
  assert.equal(view.report?.filter, "open-panes", "a same-filter result must publish even if the 15s timer fired");
  assert.equal(view.report?.generatedAtMs, 1);
  const second = host.pending("collect")[0];
  assert.ok(second, `the timer must queue one follow-up: ${host.debug()}`);
  await host.advance(VISIBLE_MS);
  assert.equal(host.pending("collect").length, 1);
  second.settle(jsonResult(report("open-panes", { generatedAtMs: 2 })));
  await flush();
  assert.equal(view.report?.generatedAtMs, 2, "the next slow collect must publish too, not starve");
  assert.equal(host.pending("collect").length, 1, "a later tick still queues exactly one follow-up");
});

test("the usage panel does not call notify --json", async () => {
  const view = new Usage();
  view.init({}, host.cx);
  await flush();
  const [collect] = host.pending("collect");
  assert.ok(collect, `init collect missing: ${host.debug()}`);
  collect.settle(jsonResult(report("open-panes")));
  await flush();
  await host.advance(60000);
  assert.equal(host.count("notify"), 0, "toast polling belongs to tabs; the panel must not schedule notify --json");
});
