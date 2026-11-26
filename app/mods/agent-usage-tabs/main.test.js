// Tabs owns toast polling (review blocker 1). Runs the real tabs mod.
// node --experimental-vm-modules --test 'app/**/*.test.js'
import { test, before, beforeEach, afterEach } from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { OPEN_COMMAND } from "../agent-usage/model.js";
import {
  Host, loadDefault, flush, layoutWith, jsonResult, LOCAL_JSON, HOME, VISIBLE_MS,
} from "../../test-support/host-harness.js";

const tabsMain = fileURLToPath(new URL("./main.js", import.meta.url));
const host = new Host();
const DUE = {
  providerId: "claude", profileLabel: "personal", window: "primary", bucket: 50,
  resetsAt: 1793730000, level: "warning", title: "Session limit · personal", body: "50% remaining",
};
const OTHER = {
  providerId: "codex", profileLabel: "work", window: "primary", bucket: 10,
  resetsAt: 1793731000, level: "error", title: "Codex limit · work", body: "10% remaining",
};

let Tabs;

before(async () => {
  host.installGlobals();
  Tabs = await loadDefault(tabsMain, host);
});

beforeEach(() => {
  host.reset();
  host.installGlobals();
  // Usage sits in a hidden column and is never constructed here.
  host.layout = layoutWith({ panelHidden: false, active: "files" });
});

afterEach(() => host.restoreGlobals());

function assertToast(actual, toast, action = OPEN_COMMAND) {
  assert.equal(actual?.title, toast.title);
  assert.equal(actual?.body, toast.body);
  assert.equal(actual?.level, toast.level);
  assert.equal(actual?.action?.name, action);
}

async function boot() {
  const view = new Tabs();
  view.init({}, host.cx);
  await flush();
  return view;
}

test("tabs posts a due notify --json toast at init without opening Usage", async () => {
  await boot();
  const [call] = host.pending("notify");
  assert.ok(call, `init must poll notify --json while Usage stays unopened: ${host.debug()}`);
  assert.ok(call.args.includes("--json"));
  assert.equal(host.count("collect"), 0, "tabs must not start a collect scheduler");
  call.settle(jsonResult({ version: 1, toasts: [DUE] }));
  await flush();
  assert.equal(host.nativeNotifies.length, 1);
  assertToast(host.nativeNotifies[0], DUE);
});

test("tabs does not poll again before 15s and does poll at 15s", async () => {
  await boot();
  for (const call of host.pending("notify")) call.settle(jsonResult({ version: 1, toasts: [] }));
  await flush();
  const before = host.count("notify");
  await host.advance(VISIBLE_MS - 1);
  assert.equal(host.count("notify"), before, "must not poll before the 15s tick");
  await host.advance(1);
  assert.equal(host.count("notify"), before + 1, "the always-shown tabs timer must poll notify --json at 15s");
});

test("tabs notify stays single-flight across a timer tick", async () => {
  await boot();
  const [first] = host.pending("notify");
  assert.ok(first, `init poll missing: ${host.debug()}`);
  await host.advance(VISIBLE_MS);
  assert.equal(host.pending("notify").length, 1, "a timer tick must not start a second notify while one is in flight");
  assert.equal(host.count("notify"), 1);
  first.settle(jsonResult({ version: 1, toasts: [DUE] }));
  await flush();
  assert.equal(host.nativeNotifies.length, 1);
  for (const call of host.pending("notify")) call.settle(jsonResult({ version: 1, toasts: [] }));
  await flush();
  const mid = host.count("notify");
  await host.advance(VISIBLE_MS);
  assert.ok(host.count("notify") > mid, "the next 15s tick must poll again after the in-flight notify finishes");
});

test("tabs loads usagebar paths from the panel local.json", async () => {
  host.files.set(LOCAL_JSON, JSON.stringify({ binary: "/opt/usagebar", configDir: "/opt/agent-usage" }));
  await boot();
  const [call] = host.pending("notify");
  assert.ok(call, `init poll missing: ${host.debug()}`);
  assert.equal(call.binary, "/opt/usagebar");
  assert.equal(call.env.HERDR_PLUGIN_CONFIG_DIR, "/opt/agent-usage");
});

test("tabs falls back to the panel default paths when local.json is missing", async () => {
  await boot();
  const [call] = host.pending("notify");
  assert.ok(call, `missing local.json must still poll: ${host.debug()}`);
  assert.equal(call.binary, `${HOME}/.local/bin/usagebar`);
  assert.equal(call.env.HERDR_PLUGIN_CONFIG_DIR, `${HOME}/.config/agent-usage`);
});

test("tabs toast action uses local.json openCommand", async () => {
  host.files.set(LOCAL_JSON, JSON.stringify({
    binary: "/opt/usagebar", configDir: "/opt/agent-usage", openCommand: "layout.custom-open",
  }));
  await boot();
  const [call] = host.pending("notify");
  assert.ok(call, `init poll missing: ${host.debug()}`);
  call.settle(jsonResult({ version: 1, toasts: [DUE] }));
  await flush();
  assertToast(host.nativeNotifies[0], DUE, "layout.custom-open");
});

test("tabs toast action defaults to the open-usage command without a collect report", async () => {
  host.files.set(LOCAL_JSON, JSON.stringify({ binary: "/opt/usagebar", configDir: "/opt/agent-usage" }));
  await boot();
  const [call] = host.pending("notify");
  assert.ok(call, `init poll missing: ${host.debug()}`);
  call.settle(jsonResult({ version: 1, toasts: [DUE] }));
  await flush();
  assertToast(host.nativeNotifies[0], DUE, OPEN_COMMAND);
});

test("tabs posts every due toast from one notify --json response", async () => {
  await boot();
  const [call] = host.pending("notify");
  assert.ok(call, `init poll missing: ${host.debug()}`);
  call.settle(jsonResult({ version: 1, toasts: [DUE, OTHER] }));
  await flush();
  assert.equal(host.nativeNotifies.length, 2);
  assertToast(host.nativeNotifies[0], DUE);
  assertToast(host.nativeNotifies[1], OTHER);
});

test("a failed notify --json does not stop the next 15s poll", async () => {
  await boot();
  for (const call of host.pending("notify")) call.settle({ code: 1, stdout: "", stderr: "usagebar exited 1" });
  await flush();
  assert.equal(host.nativeNotifies.length, 0);
  const before = host.count("notify");
  await host.advance(VISIBLE_MS);
  assert.equal(host.count("notify"), before + 1, "notify failure must not cancel the tabs timer");
});

test("an empty notify --json posts nothing and the following due toast still posts", async () => {
  await boot();
  const [first] = host.pending("notify");
  assert.ok(first, `init poll missing: ${host.debug()}`);
  first.settle(jsonResult({ version: 1, toasts: [] }));
  await flush();
  assert.equal(host.nativeNotifies.length, 0);
  await host.advance(VISIBLE_MS);
  const [second] = host.pending("notify");
  assert.ok(second, `15s poll missing: ${host.debug()}`);
  second.settle(jsonResult({ version: 1, toasts: [DUE] }));
  await flush();
  assert.equal(host.nativeNotifies.length, 1);
  assertToast(host.nativeNotifies[0], DUE);
});
