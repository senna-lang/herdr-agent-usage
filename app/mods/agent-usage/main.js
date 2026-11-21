// Agent Usage for herdr-app: the herdr-agent-usage plugin's limits pane as a
// panel in the right column (docs/design/herdr-app/SPEC.md).
//
// Data comes from the usagebar binary: `collect --format v1` for the panel
// (docs/collect-json-v1.md) and `notify --json` for toasts. It runs every 15 s
// while the panel shows and every 60 s while hidden, and sooner when a folder
// the report names in `watchDirs` changes. Paths come from local.json next to
// this file (written by scripts/herdr-app-install.sh), else the defaults below.
// The pure data-to-display logic lives in ./model.js (tested with `node --test`).

import { View, div, svg, image } from "gpui-kit";
import { v_flex, h_flex } from "gpui-base";
import { exec, home, appHome, readTextFile, watchFolder, notify, agents, panes, layout, theme, settings } from "herdr";
import {
  FAMILIES, bandColors, fillWidth, openPanes, groupAccounts, groupName, accountTitle, hint, percentWord,
  shareLine, noDataText, metaText, calloutParts, compactName, compactPlaceholder, lowCacheText, emptyStateTitle,
  footerText, errorText, toastNotifications,
} from "./model.js";

const VISIBLE_MS = 15000;
const HIDDEN_EVERY = 4; // ticks: 60 s while hidden
const WATCH_DEBOUNCE_MS = 3000;

function find(node, id) {
  if (node?.id === id) return node;
  for (const child of node?.children ?? []) {
    const found = find(child, id);
    if (found) return found;
  }
  return null;
}

function shown() {
  const root = layout(), panel = find(root, "panel"), body = find(root, "panel-body");
  return Boolean(panel && !panel.hidden && body?.active === "usage");
}

// The monospace face of the user's terminal, for tags and code.
const monoFont = () => settings().font_family ?? "JetBrains Mono";

export default class AgentUsage extends View {
  init(_props, cx) {
    this.handle = cx.focus_handle();
    this.report = null; this.error = null; this.busy = false; this.updatedAt = null;
    this.all = localStorage.getItem("filter") === "all";
    this.compact = localStorage.getItem("view") === "compact";
    this.watching = new Set(); this.lastWatchRefresh = 0; this.tick = 0;
    this.paths = { binary: `${home()}/.local/bin/usagebar`, configDir: `${home()}/.config/agent-usage` };
    cx.spawn(async (cx) => {
      try {
        const local = JSON.parse((await readTextFile(`${appHome()}/mods/agent-usage/local.json`)) ?? "{}");
        this.paths = { ...this.paths, ...local };
      } catch { /* defaults */ }
      this.refresh(cx, true);
    });
    cx.timer.every(VISIBLE_MS, (cx) => {
      this.tick += 1;
      if (shown() || this.tick % HIDDEN_EVERY === 0) this.refresh(cx, true);
    });
  }

  usagebar(args) {
    return exec("/usr/bin/env", [`HERDR_PLUGIN_CONFIG_DIR=${this.paths.configDir}`, this.paths.binary, ...args]);
  }

  refresh(cx, withToasts) {
    if (this.busy) return;
    this.busy = true;
    const panesArg = JSON.stringify(openPanes(agents(), panes()));
    cx.spawn(async (cx) => {
      try {
        const r = await this.usagebar(["collect", "--format", "v1", ...(this.all ? ["--all"] : []), "--panes", panesArg]);
        if (r.code !== 0) throw new Error((r.stderr || r.stdout).trim() || `usagebar exited ${r.code}`);
        const report = JSON.parse(r.stdout);
        if (report.version !== 1) throw new Error(`unsupported usagebar report version ${report.version}`);
        this.report = report; this.error = null; this.updatedAt = Date.now();
        this.watch(report.watchDirs ?? [], cx);
        if (withToasts) await this.postToasts(panesArg);
      } catch (e) {
        this.error = errorText(e?.message ?? e, this.paths.binary, home());
      } finally { this.busy = false; cx.notify(); }
    });
    cx.notify();
  }

  async postToasts(panesArg) {
    const r = await this.usagebar(["notify", "--json", "--panes", panesArg]);
    if (r.code !== 0) return;
    for (const n of toastNotifications(JSON.parse(r.stdout).toasts, this.report, this.paths.openCommand)) notify(n);
  }

  // Re-collect soon after a cache or rollout folder changes. A folder that
  // can't be watched (missing, too big) just leaves the timer in charge.
  watch(dirs, cx) {
    for (const dir of dirs) {
      if (this.watching.has(dir)) continue;
      this.watching.add(dir);
      cx.spawn(async (cx) => {
        try {
          for (;;) {
            await watchFolder(dir);
            if (Date.now() - this.lastWatchRefresh < WATCH_DEBOUNCE_MS) continue;
            this.lastWatchRefresh = Date.now();
            this.refresh(cx, true);
          }
        } catch { this.watching.delete(dir); }
      });
    }
  }

  setFilter(all, cx) {
    if (this.all === all) return;
    this.all = all; localStorage.setItem("filter", all ? "all" : "open");
    this.report = null; this.refresh(cx, false);
  }

  setCompact(compact, cx) {
    this.compact = compact; localStorage.setItem("view", compact ? "compact" : "full"); cx.notify();
  }

  render(cx) {
    const t = theme(), now = Date.now(), groups = groupAccounts(this.report);
    const count = groups.reduce((n, g) => n + g.members.length, 0);
    return v_flex().id("agent-usage").size_full().min_h(0).rounded(10).overflow_hidden().bg(t.background).border_1().border_color(t.border)
      .text_color(t.foreground).text_size(13).track_focus(this.handle)
      .on_mouse_down("left", () => this.handle.focus())
      .on_key_down((e, cx) => { if (e.key === "r") { this.refresh(cx, true); cx.stop_propagation(); } })
      .child(this.header(t, count, cx))
      .when(Boolean(this.error), (el) => el.child(div().px(14).py(6).text_size(11.5).text_color(t.ansi1).border_b(1).border_color(t.border).child(this.error)))
      .child(count === 0 ? this.empty(t) : v_flex().id("agent-usage-list").flex_1().min_h(0).overflow_y_scrollbar()
        .when(this.compact, (el) => el.py(6).children(groups.flatMap((g) => g.members.map((p) => this.compactRow(p, g, t)))))
        .when(!this.compact, (el) => el.children(groups.map((g, i) => this.group(g, i, t, now))))
        .when((this.report?.lowCachePanes ?? []).length > 0, (el) => el.child(div().px(14).py(8).text_size(11.5).text_color(t.ansi1)
          .child(lowCacheText(this.report)))))
      .child(this.footer(t, cx));
  }

  header(t, count, cx) {
    const seg = (id, label, on, action) => div().id(id).h(22).px(8).flex().items_center().rounded(5).text_size(12)
      .bg(on ? t.raised : t.background).border_1().border_color(on ? t.border : t.background).text_color(on ? t.foreground : t.muted)
      .cursor_pointer().on_click(action).child(label);
    return h_flex().h(42).flex_none().px(14).gap(8).items_center().border_b(1).border_color(t.border)
      .child(div().font_semibold().child("Agent Usage"))
      .when(count > 0, (el) => el.child(div().px(5).py(1).rounded(4).border_1().border_color(t.border).text_size(10).text_color(t.muted)
        .child(`${count} ${count === 1 ? "account" : "accounts"}`)))
      .child(div().flex_1())
      .child(h_flex().p(2).gap(2).rounded(7).bg(t.chrome)
        .child(seg("filter-open", "Open panes", !this.all, (_e, cx) => this.setFilter(false, cx)))
        .child(seg("filter-all", "All", this.all, (_e, cx) => this.setFilter(true, cx))));
  }

  icon(family, t, size = 16) {
    const f = FAMILIES[family];
    if (!f) return div().size(size).flex_none();
    return f.tint ? svg(f.icon).size(size).flex_none().text_color(t.foreground) : image(f.icon).size(size).flex_none();
  }

  group(g, index, t, now) {
    const first = g.members[0], grouped = g.members.length > 1;
    // A lone account is headed by its provider (a profile label such as
    // "codex" is just the account's name and adds nothing on its own).
    const name = groupName(g);
    const heading = h_flex().h(34).gap(9).items_center()
      .child(this.icon(g.family, t))
      .child(div().font_semibold().child(name))
      .when(!grouped && Boolean(first.planType), (el) => el.child(div().text_color(t.muted).child(`· ${first.planType}`)))
      .child(div().flex_1())
      .child(div().text_size(grouped ? 12 : 11).text_color(t.dim).max_w(240).truncate()
        .child(grouped ? `${g.members.length} accounts` : hint(first, home())));
    return v_flex().id(`group-${index}`).px(14).pt(6).pb(10).when(index > 0, (el) => el.border_t(1).border_color(t.border))
      .child(heading)
      .children(g.members.map((p) => this.account(p, grouped, t, now)));
  }

  account(p, grouped, t, now) {
    const bands = bandColors(t);
    const stale = p.status === "stale";
    const word = percentWord(this.report);
    const rows = [];
    if (grouped) {
      rows.push(h_flex().h(26).gap(5).items_center().text_size(12.5)
        .child(div().font_medium().child(accountTitle(p)))
        .when(Boolean(p.planType), (el) => el.child(div().text_color(t.muted).child(`· ${p.planType}`)))
        .child(div().flex_1())
        .child(div().text_size(11).text_color(t.dim).max_w(240).truncate().child(hint(p, home()))));
    }
    for (const w of p.windows) {
      const band = bands[w.band] ?? t.muted;
      rows.push(h_flex().h(22).gap(8).items_center().when(stale, (el) => el.opacity(0.45))
        .child(div().w(24).flex_none().font_family(monoFont()).text_size(11.5).text_color(t.muted).child(w.tag))
        .child(div().flex_1().min_w(0).h(6).rounded(3).bg(band + "26")
          .child(div().h_full().rounded(3).bg(band).w(fillWidth(w))))
        .child(div().w(62).flex_none().text_right().text_size(12).text_color(band).child(`${w.displayPercent}% ${word}`))
        .child(div().w(54).flex_none().text_right().text_size(11.5).text_color(t.dim).child(w.resetIn)));
      if (w.runOut?.text) {
        rows.push(div().pl(32).h(20).flex().items_center().text_size(11.5).text_color(t.ansi1).child(`⚠ ${w.runOut.text}`));
      }
    }
    // Hidden unless local.json sets "showPaneShare": true (the per-pane split is noisy).
    const share = shareLine(p, this.paths.showPaneShare);
    if (share) {
      rows.push(h_flex().gap(6).py(2).items_start().text_size(11.5)
        .child(div().flex_none().text_color(t.dim).child(share.tag))
        .child(div().flex_1().min_w(0).text_color(t.muted).child(share.text)));
    }
    if (p.status === "no-data") {
      rows.push(div().py(3).text_size(12).text_color(t.dim).child(noDataText(p, home())));
      return v_flex().pl(25).when(grouped, (el) => el.pt(4)).children(rows);
    }
    // The boxed callout is for something the user must fix (a login).
    if (p.status === "needs-login" && p.note) rows.push(this.callout(p.note, t));
    else if (p.note && !stale) rows.push(div().py(2).text_size(11.5).text_color(t.dim).child(p.note));
    rows.push(div().py(2).text_size(11).text_color(stale ? t.ansi3 : t.dim).child(metaText(p, now, home())));
    return v_flex().pl(25).when(grouped, (el) => el.pt(4)).children(rows);
  }

  // A note with the command to run drawn as code (`grok login`).
  callout(note, t) {
    const parts = calloutParts(note);
    return h_flex().my(4).px(10).py(7).gap(0).flex_wrap().items_center().rounded(7).bg("#ffffff08").border_1().border_color(t.border)
      .text_size(12).text_color(t.muted)
      // Words are separate items so a long note wraps around the code.
      .children(parts.flatMap((part, i) => (i % 2
        ? [div().mr(4).px(4).rounded(4).bg(t.raised).text_color(t.foreground).font_family(monoFont()).child(part)]
        : part.split(/\s+/).filter(Boolean).map((word) => div().mr(4).child(word)))));
  }

  compactRow(p, g, t) {
    const bands = bandColors(t);
    const name = compactName(p, g);
    const mono = monoFont();
    return h_flex().h(30).px(14).gap(8).items_center().when(p.status === "stale", (el) => el.opacity(0.45))
      .child(this.icon(g.family, t))
      .child(div().flex_1().min_w(0).truncate().text_color(t.muted).child(name))
      .children(p.windows.map((w) => h_flex().gap(4).items_center().flex_none()
        .child(div().font_family(mono).text_size(11.5).text_color(t.dim).child(w.tag))
        .child(div().w(18).h(6).rounded(3).bg(bands[w.band] + "26").child(div().h_full().rounded(3).bg(bands[w.band]).w(fillWidth(w))))
        .child(div().w(34).text_right().text_size(12).text_color(bands[w.band]).child(`${w.displayPercent}%`))))
      .when(p.windows.some((w) => w.runOut?.text), (el) => el.child(div().text_size(12).text_color(t.ansi1).child("⚠")))
      .when(!p.windows.length, (el) => el.child(div().text_size(11.5).text_color(t.dim)
        .child(compactPlaceholder(p))));
  }

  empty(t) {
    const open = !this.all;
    const title = emptyStateTitle(this.report, this.error, this.all);
    return v_flex().flex_1().min_h(0).items_center().justify_center().gap(6).px(24)
      .child(div().font_semibold().text_size(14).child(title))
      .when(Boolean(this.report) && open, (el) => el.child(div().text_center().text_size(12.5).text_color(t.dim)
        .child("Limits show for providers with an open agent pane. Choose All to see every configured account.")));
  }

  footer(t, cx) {
    const seg = (id, label, on, action) => div().id(id).px(6).h(20).flex().items_center().rounded(5).text_size(11)
      .text_color(on ? t.foreground : t.dim).when(on, (el) => el.bg(t.raised)).cursor_pointer().on_click(action).child(label);
    return h_flex().h(40).flex_none().px(14).gap(8).items_center().border_t(1).border_color(t.border).text_size(11.5).text_color(t.dim)
      .child(div().flex_1().min_w(0).truncate().child(footerText(this.updatedAt, this.busy, VISIBLE_MS)))
      .child(h_flex().gap(1)
        .child(seg("view-full", "Full", !this.compact, (_e, cx) => this.setCompact(false, cx)))
        .child(seg("view-compact", "Compact", this.compact, (_e, cx) => this.setCompact(true, cx))))
      .child(h_flex().id("refresh").h(26).px(10).gap(6).items_center().rounded(7).bg(t.raised).text_size(12).text_color(t.foreground)
        .cursor_pointer().hover((s) => s.bg(t.hover)).on_click((_e, cx) => this.refresh(cx, true))
        .child(div().text_color(this.busy ? t.dim : t.foreground).child("Refresh"))
        .child(div().font_family(monoFont()).text_size(11).text_color(t.dim).child("r")));
  }
}
