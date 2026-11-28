// Test-only host for the real herdr-app mods. Product files are not edited.
// Load with vm.SourceTextModule so gpui-kit / gpui-base / herdr resolve to
// stubs. Time advances only through Host.advance; there is no wall clock.
import vm from "node:vm";
import { readFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";

export const HOME = "/home/user-1";
export const APP_HOME = "/herdr-app";
export const VISIBLE_MS = 15000;
export const HIDDEN_MS = 60000;
export const DEBOUNCE_MS = 3000;
export const LOCAL_JSON = `${APP_HOME}/mods/agent-usage/local.json`;

const ORIGINALS = {
  now: Date.now,
  setTimeout: globalThis.setTimeout,
  clearTimeout: globalThis.clearTimeout,
  setInterval: globalThis.setInterval,
  clearInterval: globalThis.clearInterval,
  localStorage: Object.prototype.hasOwnProperty.call(globalThis, "localStorage")
    ? globalThis.localStorage
    : undefined,
  hadLocalStorage: Object.prototype.hasOwnProperty.call(globalThis, "localStorage"),
};

export function layoutWith({ panelHidden = false, active = "files" } = {}) {
  return {
    id: "root",
    children: [
      { id: "panel", hidden: panelHidden },
      { id: "panel-body", active },
    ],
  };
}

export function jsonResult(value, code = 0, stderr = "") {
  return { code, stdout: JSON.stringify(value), stderr };
}

export function parentDir(file) {
  const i = String(file).lastIndexOf("/");
  return i <= 0 ? String(file) : String(file).slice(0, i);
}

function norm(p) {
  const s = String(p);
  return s.length > 1 ? s.replace(/\/+$/, "") : s;
}

// Every fluent call must return this proxy. Returning the raw function drops
// the traps, so render's `.id(...).size_full()` throws on the next property.
function chain() {
  const proxy = new Proxy(function () { return proxy; }, {
    get(_target, prop) {
      if (prop === "then") return undefined;
      return proxy;
    },
    apply() { return proxy; },
  });
  return proxy;
}

// Current panel: exec("/usr/bin/env", ["HERDR_PLUGIN_CONFIG_DIR=...", binary, ...args]).
// A later host may pass exec(binary, args, { env }). Both are accepted here.
export function parseExec(command, args, options = {}) {
  const argv = [command, ...(Array.isArray(args) ? args : args == null ? [] : [args])];
  const env = { ...(options?.env ?? {}) };
  const words = [];
  for (const part of argv) {
    if (typeof part !== "string" || part === "/usr/bin/env" || part === "env") continue;
    const eq = part.indexOf("=");
    if (eq > 0 && words.length === 0 && /^[A-Z_][A-Z0-9_]*$/.test(part.slice(0, eq))) {
      env[part.slice(0, eq)] = part.slice(eq + 1);
      continue;
    }
    words.push(part);
  }
  let binary = "";
  let cmdArgs = words;
  if (words[0] !== "collect" && words[0] !== "notify" && words.length) {
    binary = words[0];
    cmdArgs = words.slice(1);
  }
  const sub = cmdArgs.find((w) => w === "collect" || w === "notify") ?? "";
  return { binary, args: cmdArgs, env, sub, raw: { command, args, options } };
}

export class Host {
  constructor() {
    this.storage = {
      _m: new Map(),
      getItem: (k) => (this.storage._m.has(k) ? this.storage._m.get(k) : null),
      setItem: (k, v) => this.storage._m.set(k, String(v)),
      removeItem: (k) => this.storage._m.delete(k),
      clear: () => this.storage._m.clear(),
    };
    this.cx = {
      spawn: (fn) => {
        const p = Promise.resolve().then(() => fn(this.cx));
        this.spawns.push(p);
        p.catch((err) => { this.spawnErrors.push(err); });
        return p;
      },
      timer: {
        every: (ms, cb) => {
          this.intervals.push({ ms, elapsed: 0, cb });
        },
      },
      notify: () => { this.uiNotifies += 1; },
      focus_handle: () => ({ focus() {} }),
      stop_propagation: () => {},
    };
    this.reset();
  }

  reset() {
    this.now = 1_700_000_000_000;
    this.files = new Map();
    this.calls = [];
    this.waiters = [];
    this.pendingEvents = new Map();
    this.watched = new Set();
    this.watchLog = [];
    this.reads = [];
    this.intervals = [];
    this.timeouts = new Map();
    this.seq = 0;
    this.uiNotifies = 0;
    this.nativeNotifies = [];
    this.spawns = [];
    this.spawnErrors = [];
    this.commandWaiters = [];
    this.layout = layoutWith();
    this.autoResolve = false;
    this.onRead = null;
    this.storage.clear();
    this.agents = () => [];
    this.panes = () => [];
  }

  installGlobals() {
    Date.now = () => this.now;
    globalThis.localStorage = this.storage;
    globalThis.setTimeout = (fn, ms = 0, ...args) => {
      const id = ++this.seq;
      this.timeouts.set(id, { id, ms, elapsed: 0, fn: () => fn(...args) });
      return id;
    };
    globalThis.clearTimeout = (id) => { this.timeouts.delete(id); };
    globalThis.setInterval = (fn, ms = 0, ...args) => {
      const id = ++this.seq;
      this.intervals.push({ id, ms, elapsed: 0, cb: () => fn(...args) });
      return id;
    };
    globalThis.clearInterval = (id) => {
      const i = this.intervals.findIndex((t) => t.id === id);
      if (i >= 0) this.intervals.splice(i, 1);
    };
  }

  restoreGlobals() {
    Date.now = ORIGINALS.now;
    globalThis.setTimeout = ORIGINALS.setTimeout;
    globalThis.clearTimeout = ORIGINALS.clearTimeout;
    globalThis.setInterval = ORIGINALS.setInterval;
    globalThis.clearInterval = ORIGINALS.clearInterval;
    if (ORIGINALS.hadLocalStorage) globalThis.localStorage = ORIGINALS.localStorage;
    else delete globalThis.localStorage;
  }

  count(sub) {
    return this.calls.filter((c) => c.sub === sub).length;
  }
  pending(sub) {
    return this.calls.filter((c) => c.sub === sub && !c.settled);
  }
  debug() {
    return JSON.stringify({
      collects: this.count("collect"),
      notifies: this.count("notify"),
      pending: this.calls.filter((c) => !c.settled).map((c) => c.sub),
      watched: [...this.watched],
      errors: this.spawnErrors.map((e) => String(e?.message ?? e)),
    });
  }

  noteExec(command, args, options) {
    const parsed = parseExec(command, args, options);
    let resolve;
    const promise = new Promise((res) => { resolve = res; });
    const call = { ...parsed, settled: false, promise, resolve };
    call.settle = (result) => {
      if (call.settled) throw new Error(`exec already settled: ${call.sub}`);
      call.settled = true;
      call.resolve(result);
    };
    this.calls.push(call);
    if (this.autoResolve && (call.sub === "collect" || call.sub === "notify")) {
      const result = call.sub === "notify"
        ? jsonResult({ version: 1, toasts: [] })
        : this.autoResolve;
      queueMicrotask(() => { if (!call.settled) call.settle(result); });
    }
    return promise;
  }

  watchFolder(dir) {
    const key = norm(dir);
    this.watched.add(key);
    this.watchLog.push(key);
    const queued = this.pendingEvents.get(key);
    if (queued?.length) return Promise.resolve(queued.shift());
    return new Promise((resolve, reject) => {
      this.waiters.push({ dir: key, resolve, reject });
    });
  }

  // Folder watches resolve an unknown payload unless a test passes one.
  // Tests must not treat a filename field as something the host can filter on.
  emitWatch(dir, payload = undefined) {
    const key = norm(dir);
    const idx = this.waiters.findIndex((w) => w.dir === key);
    if (idx >= 0) {
      const [waiter] = this.waiters.splice(idx, 1);
      waiter.resolve(payload);
      return;
    }
    const queued = this.pendingEvents.get(key) ?? [];
    queued.push(payload);
    this.pendingEvents.set(key, queued);
  }

  async readTextFile(p) {
    const value = this.files.has(p) ? this.files.get(p) : null;
    this.reads.push({ path: p, value });
    this.onRead?.(p, value);
    return value;
  }

  notify(n) { this.nativeNotifies.push(n); }

  async advance(ms) {
    this.now += ms;
    const jobs = [];
    for (const t of this.intervals.slice()) {
      if (!(t.ms > 0)) continue;
      t.elapsed += ms;
      while (t.elapsed >= t.ms) {
        t.elapsed -= t.ms;
        jobs.push(t.cb);
      }
    }
    for (const t of [...this.timeouts.values()]) {
      t.elapsed += ms;
      if (t.elapsed >= t.ms) {
        this.timeouts.delete(t.id);
        jobs.push(t.fn);
      }
    }
    for (const fn of jobs) fn(this.cx);
    await flush();
  }
}

export async function flush() {
  for (let i = 0; i < 80; i++) await Promise.resolve();
}

function synthetic(specifier, names, init) {
  return new vm.SyntheticModule(names, init, { identifier: `stub:${specifier}` });
}

function herdrModule(host) {
  return synthetic("herdr", [
    "exec", "home", "appHome", "readTextFile", "watchFolder", "notify",
    "agents", "panes", "layout", "theme", "settings",
    "spaces", "run", "openOverlay", "window", "shared", "nextCommand",
  ], function () {
    this.setExport("exec", (command, args, options) => host.noteExec(command, args, options));
    this.setExport("home", () => HOME);
    this.setExport("appHome", () => APP_HOME);
    this.setExport("readTextFile", (p) => host.readTextFile(p));
    this.setExport("watchFolder", (dir) => host.watchFolder(dir));
    this.setExport("notify", (n) => host.notify(n));
    this.setExport("agents", () => host.agents());
    this.setExport("panes", () => host.panes());
    this.setExport("layout", () => host.layout);
    this.setExport("theme", () => ({ foreground: "#fff", muted: "#888", dim: "#666", border: "#333", background: "#111", chrome: "#222", hover: "#333", raised: "#444", selected: "#555", ansi1: "red", ansi2: "green", ansi3: "yellow", working: "#0ff", done: "#0f0", blocked: "#f80" }));
    this.setExport("settings", () => ({ font_family: "Test Mono", zoom: 1 }));
    this.setExport("spaces", () => [{ id: "s1", active: true, tabs: [] }]);
    this.setExport("run", () => {});
    this.setExport("openOverlay", () => {});
    this.setExport("window", () => ({ platform: "macos", titlebar: "native", fullscreen: false, buttons: false }));
    this.setExport("shared", () => ["files", "changes"]);
    this.setExport("nextCommand", () => new Promise((resolve) => { host.commandWaiters.push(resolve); }));
  });
}

function kitModule() {
  return synthetic("gpui-kit", ["View", "div", "svg", "image"], function () {
    class View {}
    this.setExport("View", View);
    this.setExport("div", chain);
    this.setExport("svg", chain);
    this.setExport("image", chain);
  });
}

function baseModule() {
  return synthetic("gpui-base", ["v_flex", "h_flex"], function () {
    this.setExport("v_flex", chain);
    this.setExport("h_flex", chain);
  });
}

export async function loadDefault(file, host) {
  const cache = new Map();
  const stubs = new Map([
    ["herdr", herdrModule(host)],
    ["gpui-kit", kitModule()],
    ["gpui-base", baseModule()],
  ]);
  async function linker(specifier, referencingModule) {
    if (stubs.has(specifier)) return stubs.get(specifier);
    const href = new URL(specifier, referencingModule.identifier).href;
    if (cache.has(href)) return cache.get(href);
    const source = await readFile(new URL(href), "utf8");
    const mod = new vm.SourceTextModule(source, { identifier: href });
    cache.set(href, mod);
    await mod.link(linker);
    return mod;
  }
  const identifier = pathToFileURL(file).href;
  const source = await readFile(file, "utf8");
  const mod = new vm.SourceTextModule(source, { identifier });
  cache.set(identifier, mod);
  await mod.link(linker);
  await mod.evaluate();
  return mod.namespace.default;
}
