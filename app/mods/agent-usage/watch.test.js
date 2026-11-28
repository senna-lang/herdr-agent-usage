// Fresh-data watches (review blocker 7). Runs the real panel mod.
// node --experimental-vm-modules --test 'app/**/*.test.js'
import { test, before, beforeEach, afterEach } from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import {
  Host, loadDefault, flush, layoutWith, jsonResult, LOCAL_JSON, parentDir, DEBOUNCE_MS, HIDDEN_MS,
} from "../../test-support/host-harness.js";

const usageMain = fileURLToPath(new URL("./main.js", import.meta.url));
const host = new Host();
const CACHE = "/home/user-1/.claude/herdr-usagebar/claude-limits-latest.json";
const CACHE2 = "/home/user-1/.claude-work/herdr-usagebar/claude-limits-latest.json";
const HISTORY = "/home/user-1/.claude/herdr-usagebar/usage-history.json";
const STATE = "/home/user-1/.claude/herdr-usagebar/rate-limit-state.json";
const PROVIDER_STATE = "/home/user-1/.claude/herdr-usagebar/provider-limit-notify-state.json";
const SESSIONS = "/home/user-1/.codex/sessions";

let Usage;

before(async () => {
  host.installGlobals();
  Usage = await loadDefault(usageMain, host);
});

beforeEach(() => {
  host.reset();
  host.installGlobals();
  host.files.set(LOCAL_JSON, JSON.stringify({ binary: "/opt/usagebar", configDir: "/opt/agent-usage" }));
  host.files.set(CACHE, "v1");
  host.files.set(CACHE2, "w1");
  host.files.set(HISTORY, "h1");
  host.files.set(STATE, "s1");
  host.files.set(PROVIDER_STATE, "p1");
});

afterEach(() => host.restoreGlobals());

function report(extra = {}) {
  return {
    version: 1,
    generatedAtMs: 1000,
    filter: "open-panes",
    limitPercent: "remaining",
    emptyMessage: "(no agent panes open)",
    providers: [],
    apiUsage: [],
    lowCachePanes: [],
    watchDirs: extra.watchDirs ?? [SESSIONS],
    watchFiles: extra.watchFiles ?? [CACHE, CACHE2],
  };
}

async function boot({ shown = true } = {}) {
  host.layout = layoutWith({ panelHidden: false, active: shown ? "usage" : "files" });
  const view = new Usage();
  view.init({}, host.cx);
  await flush();
  const [collect] = host.pending("collect");
  assert.ok(collect, `init collect missing: ${host.debug()}`);
  collect.settle(jsonResult(report()));
  await flush();
  // The old panel still awaits notify before clearing busy. Drain it so later
  // watches are observed in the steady state, not dropped behind that call.
  for (const call of host.pending("notify")) call.settle(jsonResult({ version: 1, toasts: [] }));
  await flush();
  return view;
}

async function assertNoExtraCollect(since, message) {
  await flush();
  await host.advance(DEBOUNCE_MS);
  assert.equal(host.count("collect") - since, 0, message);
}

// Advances in 100 ms steps; returns how long after `origin` the next collect starts.
async function collectStartsAt(origin, since, limitMs) {
  while (host.count("collect") === since) {
    if (host.now - origin >= limitMs) return null;
    await host.advance(100);
  }
  return host.now - origin;
}

test("usage-history rewrite does not start a collect", async () => {
  await boot();
  assert.ok(host.watched.has(parentDir(CACHE)), `cache parent must be watched so a history rewrite can be ignored: ${host.debug()}`);
  const since = host.count("collect");
  host.files.set(HISTORY, "h2");
  host.emitWatch(parentDir(CACHE));
  await assertNoExtraCollect(since, "usage-history.json is written by collect and must not start another collect");
});

test("notify state rewrite does not start a collect", async () => {
  await boot();
  assert.ok(host.watched.has(parentDir(CACHE)), `cache parent must be watched: ${host.debug()}`);
  const since = host.count("collect");
  host.files.set(STATE, "s2");
  host.files.set(PROVIDER_STATE, "p2");
  host.emitWatch(parentDir(CACHE));
  await assertNoExtraCollect(since, "notify state files are written by notify --json and must not start a collect");
});

test("a visible cache file change collects within the watch debounce", async () => {
  await boot({ shown: true });
  const since = host.count("collect");
  host.files.set(CACHE, "v1\n");
  host.emitWatch(parentDir(CACHE));
  await flush();
  await host.advance(DEBOUNCE_MS);
  assert.equal(host.count("collect") - since, 1, "an exact cache content change must collect without waiting for the 15s timer");
});

test("a visible cache change collects when the folder event names a self-written file", async () => {
  await boot({ shown: true });
  const since = host.count("collect");
  host.files.set(CACHE, "v2");
  host.emitWatch(parentDir(CACHE), { filename: "usage-history.json" });
  await flush();
  await host.advance(DEBOUNCE_MS);
  assert.equal(host.count("collect") - since, 1, "folder payload may not name the changed file; fingerprint the cache path");
});

test("a folder event that names the cache file does not collect when its content is unchanged", async () => {
  await boot({ shown: true });
  assert.ok(host.watched.has(parentDir(CACHE)), `cache parent must be watched: ${host.debug()}`);
  const since = host.count("collect");
  host.emitWatch(parentDir(CACHE), { filename: "claude-limits-latest.json" });
  await assertNoExtraCollect(since, "a filename in the payload must not replace the content fingerprint");
});

test("a second Claude cache file change collects", async () => {
  await boot({ shown: true });
  const since = host.count("collect");
  host.files.set(CACHE2, "w2");
  host.emitWatch(parentDir(CACHE2));
  await flush();
  await host.advance(DEBOUNCE_MS);
  assert.equal(host.count("collect") - since, 1, "every watchFiles path must be fingerprinted, not only the first");
});

test("a cache write during watch startup is not lost", async () => {
  host.onRead = (p, value) => {
    if (host.gapUsed || p !== CACHE || value !== "v1") return;
    host.gapUsed = true;
    host.files.set(CACHE, "v2");
    host.emitWatch(parentDir(CACHE));
  };
  await boot({ shown: true });
  assert.ok(host.pending("collect").length >= 1, `a cache write that lands while the watch is armed must be collected: ${host.debug()}`);
});

test("cache files are fingerprinted when their parent watch is armed", async () => {
  await boot();
  assert.ok(host.reads.some((r) => r.path === CACHE && r.value === "v1"), "the cache file must be read to seed its fingerprint");
  assert.ok(host.reads.some((r) => r.path === CACHE2 && r.value === "w1"), "every watchFiles path must be read");
  assert.ok(host.watched.has(parentDir(CACHE)) && host.watched.has(parentDir(CACHE2)), `parents of watchFiles must be watched: ${host.debug()}`);
});

test("a trailing sessions event during debounce is not lost", async () => {
  await boot({ shown: true });
  const since = host.count("collect");
  host.emitWatch(SESSIONS);
  await flush();
  if (!host.pending("collect").length) await host.advance(DEBOUNCE_MS);
  const [first] = host.pending("collect");
  assert.ok(first, `visible sessions event must collect: ${host.debug()}`);
  await host.advance(1000);
  host.emitWatch(SESSIONS);
  await flush();
  assert.equal(host.pending("collect").length, 1, "the trailing event must not start a parallel collect");
  first.settle(jsonResult(report()));
  for (const call of host.pending("notify")) call.settle(jsonResult({ version: 1, toasts: [] }));
  await flush();
  assert.equal(host.count("collect") - since, 1, "the trailing event waits for its 3 s deadline and must not rerun as soon as the first collect settles");
  await host.advance(DEBOUNCE_MS);
  assert.equal(host.count("collect") - since, 2, "debounce may coalesce, but the trailing sessions change must still collect");
});

test("rapid sessions events coalesce to one collect", async () => {
  await boot({ shown: true });
  const since = host.count("collect");
  host.emitWatch(SESSIONS);
  host.emitWatch(SESSIONS);
  await flush();
  await host.advance(DEBOUNCE_MS);
  assert.equal(host.count("collect") - since, 1, "two sessions events inside the debounce window must not start two collects");
});

test("a visible Codex sessions event collects without a filename", async () => {
  await boot({ shown: true });
  const since = host.count("collect");
  host.emitWatch(SESSIONS, undefined);
  await flush();
  await host.advance(DEBOUNCE_MS);
  assert.equal(host.count("collect") - since, 1, "a sessions-tree event has no filename; it still means fresh rollouts");
});

test("a hidden sessions event does not collect before 60s", async () => {
  await boot({ shown: false });
  const since = host.count("collect");
  host.emitWatch(SESSIONS);
  await flush();
  await host.advance(HIDDEN_MS - 1);
  assert.equal(host.count("collect") - since, 0, "a hidden watch must not bypass the 60s cadence");
});

test("a hidden watch event does not add a collect on top of the 60s timer", async () => {
  await boot({ shown: false });
  const since = host.count("collect");
  host.autoResolve = jsonResult(report());
  host.emitWatch(SESSIONS);
  await flush();
  await host.advance(HIDDEN_MS);
  assert.equal(host.count("collect") - since, 1, "hidden dirty state is collected by the 60s timer, not by an extra watch refresh");
});

test("a hidden cache change does not bypass the 60s timer", async () => {
  await boot({ shown: false });
  assert.ok(host.watched.has(parentDir(CACHE)), `cache parent must be watched: ${host.debug()}`);
  const since = host.count("collect");
  host.files.set(CACHE, "v2");
  host.emitWatch(parentDir(CACHE));
  await flush();
  await host.advance(HIDDEN_MS - 1);
  assert.equal(host.count("collect") - since, 0, "a hidden cache change only marks dirty until the 60s timer");
});

test("a hidden panel with no watch event still collects at 60s", async () => {
  await boot({ shown: false });
  const since = host.count("collect");
  await host.advance(HIDDEN_MS - 1);
  assert.equal(host.count("collect") - since, 0);
  await host.advance(1);
  assert.equal(host.count("collect") - since, 1, "the hidden cadence stays 60s when nothing was written");
});

test("an idle watch event 800 ms after a watch start waits for the 3 s deadline", async () => {
  await boot({ shown: true });
  const since = host.count("collect");
  const origin = host.now;
  host.emitWatch(SESSIONS);
  await flush();
  const [first] = host.pending("collect");
  assert.ok(first, `first visible sessions event must collect at once: ${host.debug()}`);
  first.settle(jsonResult(report()));
  await flush();
  await host.advance(800);
  host.emitWatch(SESSIONS);
  await flush();
  assert.equal(host.count("collect") - since, 1, "an event inside the debounce window must not collect before its deadline");
  const at = await collectStartsAt(origin, host.count("collect"), 2 * DEBOUNCE_MS);
  assert.ok(at !== null && at >= DEBOUNCE_MS && at <= 2 * DEBOUNCE_MS, `trailing collect must start at or after 3 s and within one pump tick of it, got ${at}`);
  assert.equal(host.count("collect") - since, 2, "the held event must produce exactly one trailing collect");
});

test("events held during a watch-started collect do not rerun when it settles early", async () => {
  await boot({ shown: true });
  const since = host.count("collect");
  const origin = host.now;
  host.emitWatch(SESSIONS);
  await flush();
  const [first] = host.pending("collect");
  assert.ok(first, `visible sessions event must collect: ${host.debug()}`);
  await host.advance(800);
  host.emitWatch(SESSIONS);
  await flush();
  await host.advance(800);
  host.emitWatch(SESSIONS);
  await flush();
  await host.advance(400);
  first.settle(jsonResult(report()));
  await flush();
  assert.equal(host.count("collect") - since, 1, "a burst during a collect must not rerun at 2 s when that collect settles");
  const at = await collectStartsAt(origin, host.count("collect"), 2 * DEBOUNCE_MS);
  assert.ok(at !== null && at >= DEBOUNCE_MS && at <= 2 * DEBOUNCE_MS, `the burst must launch at or after its 3 s deadline, got ${at}`);
  assert.equal(host.count("collect") - since, 2, "the whole burst must coalesce into one trailing collect");
});

test("a deadline that passes during a collect launches the held event when it settles", async () => {
  await boot({ shown: true });
  const since = host.count("collect");
  host.emitWatch(SESSIONS);
  await flush();
  const [first] = host.pending("collect");
  assert.ok(first, `visible sessions event must collect: ${host.debug()}`);
  await host.advance(800);
  host.emitWatch(SESSIONS);
  await flush();
  await host.advance(3200);
  assert.equal(host.count("collect") - since, 1, "the in-flight collect must remain the only collect while its deadline passes");
  first.settle(jsonResult(report()));
  await flush();
  assert.equal(host.count("collect") - since, 2, "a deadline that passed during the collect must launch the held event as soon as it settles");
});

test("a held watch event does not launch while hidden and does not bypass the 60 s cadence", async () => {
  await boot({ shown: true });
  const since = host.count("collect");
  host.emitWatch(SESSIONS);
  await flush();
  const [first] = host.pending("collect");
  assert.ok(first, `visible sessions event must collect: ${host.debug()}`);
  first.settle(jsonResult(report()));
  await flush();
  await host.advance(800);
  host.emitWatch(SESSIONS);
  await flush();
  host.layout = layoutWith({ panelHidden: true, active: "usage" });
  await host.advance(HIDDEN_MS - 1 - 800);
  assert.equal(host.count("collect") - since, 1, "a held watch event must not launch while hidden, even after its deadline");
  await host.advance(1);
  assert.equal(host.count("collect") - since, 2, "the hidden 60 s timer collects once; the held watch event adds nothing");
});
