#!/usr/bin/env bash
# Installs the herdr-app flavour of Agent Usage (app/ in this repo) into a
# herdr-app home, or removes what it installed.
#
#   scripts/herdr-app-install.sh install     build usagebar, copy the mods and the layout
#   scripts/herdr-app-install.sh uninstall   remove exactly what install recorded
#   scripts/herdr-app-install.sh status      show what is installed where
#
# It only ever writes agent-usage* names under the herdr-app home, refuses to
# install over anything it did not install itself (a shipped mod, a personal
# file, another tool's), and runs `herdr-app check` on every mod it copies.
# The herdr TUI plugin is not needed and not touched.
#
# Environment:
#   HERDR_APP_HOME            herdr-app home (default ~/.config/herdr-app)
#   AGENT_USAGE_BIN           where the binary goes (default ~/.local/bin/usagebar)
#   AGENT_USAGE_CONFIG_DIR    usagebar config for the app (default ~/.config/agent-usage);
#                             config.toml there holds [[claude.profiles]] etc.
#   AGENT_USAGE_OPEN_COMMAND  command a toast runs to open the panel, only for a
#                             layout whose tab bar is not agent-usage-tabs (the
#                             mod's default is agent-usage-tabs.open-usage)
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
app_home="${HERDR_APP_HOME:-$HOME/.config/herdr-app}"
bin="${AGENT_USAGE_BIN:-$HOME/.local/bin/usagebar}"
config_dir="${AGENT_USAGE_CONFIG_DIR:-$HOME/.config/agent-usage}"
manifest="$config_dir/herdr-app-install.manifest"

die() { echo "herdr-app-install: $*" >&2; exit 1; }
say() { echo "herdr-app-install: $*"; }

# recorded PATH: was PATH written by a previous install?
recorded() { [[ -f "$manifest" ]] && grep -qxF "$1" "$manifest"; }

# The app's files: every mod folder and layout under app/, all agent-usage*.
sources() {
  local d f
  for d in "$repo"/app/mods/*/; do echo "mods/$(basename "$d")"; done
  for f in "$repo"/app/layouts/*.json; do echo "layouts/$(basename "$f")"; done
}

shipped() {
  # installed.json is herdr-app's own list of what it ships.
  [[ -f "$app_home/installed.json" ]] && grep -q "\"$1\"" "$app_home/installed.json"
}

preflight() {
  [[ -d "$app_home" ]] || die "no herdr-app home at $app_home (start herdr-app once, or set HERDR_APP_HOME)"
  command -v herdr-app >/dev/null || die "herdr-app is not on PATH"
  local rel name clash=0
  while read -r rel; do
    name="$(basename "$rel" .json)"
    [[ "$name" == agent-usage* ]] || die "refusing to install $rel: only agent-usage* names are allowed"
    if shipped "$name" || shipped "layout:$name"; then
      echo "  $rel: herdr-app ships this name" >&2; clash=1
    elif [[ -e "$app_home/$rel" ]] && ! recorded "$app_home/$rel"; then
      echo "  $app_home/$rel exists and was not installed by this script" >&2; clash=1
    fi
  done < <(sources)
  if [[ -e "$bin" ]] && ! recorded "$bin"; then
    echo "  $bin exists and was not installed by this script (set AGENT_USAGE_BIN)" >&2; clash=1
  fi
  [[ $clash -eq 0 ]] || die "name clash, nothing was changed"
}

install_binary() {
  bash "$repo/bin/ensure-binary.sh" --build >/dev/null
  mkdir -p "$(dirname "$bin")"
  install -m 0755 "$repo/bin/usagebar" "$bin.tmp.$$"
  mv -f "$bin.tmp.$$" "$bin"
  echo "$bin" >> "$manifest.new"
}

seed_config() {
  mkdir -p "$config_dir"
  [[ -e "$config_dir/config.toml" ]] && return 0
  cat > "$config_dir/config.toml" <<'TOML'
# usagebar config for the herdr-app Agent Usage panel
# (HERDR_PLUGIN_CONFIG_DIR points here; see README "herdr-app").

[notify]
# The panel posts toasts through herdr-app (usagebar notify --json); keep the
# herdr CLI delivery and the Claude statusLine bridge from sending them too.
delivery = "external"

# Accounts, one entry per config dir / home, e.g.:
# [[claude.profiles]]
# id = "claude-1"
# label = "claude-1"
# config_dir = "~/.claude-1"
#
# [[codex.profiles]]
# id = "codex"
# label = "codex"
# codex_home = "~/.codex"
TOML
  say "wrote $config_dir/config.toml (no accounts configured yet)"
}

# json_string quotes one value as a JSON string, escaping only backslash, quote,
# \n, \r and \t, which is enough for paths.
json_string() {
  local s="$1"
  s="${s//\\/\\\\}"; s="${s//\"/\\\"}"
  s="${s//$'\n'/\\n}"; s="${s//$'\r'/\\r}"; s="${s//$'\t'/\\t}"
  printf '"%s"' "$s"
}

# local.json tells the panel where its binary and config are. openCommand is
# written only when AGENT_USAGE_OPEN_COMMAND is set, for a layout whose tab bar
# is not agent-usage-tabs; otherwise the mod's own default applies.
write_local_json() {
  {
    printf '{\n  "binary": %s,\n  "configDir": %s' "$(json_string "$bin")" "$(json_string "$config_dir")"
    if [[ -n "${AGENT_USAGE_OPEN_COMMAND:-}" ]]; then
      printf ',\n  "openCommand": %s' "$(json_string "$AGENT_USAGE_OPEN_COMMAND")"
    fi
    printf '\n}\n'
  } > "$app_home/mods/agent-usage/local.json"
}

copy_app() {
  local rel src dst
  while read -r rel; do
    src="$repo/app/$rel"; dst="$app_home/$rel"
    if [[ -d "$src" ]]; then
      rm -rf "$dst.tmp.$$"; mkdir -p "$dst.tmp.$$"
      cp -R "$src/." "$dst.tmp.$$/"
      # herdr-app writes its own gpui-kit.d.ts / jsconfig.json into mod folders;
      # the mods' tests are not part of what gets installed.
      find "$dst.tmp.$$" -type f \( -name gpui-kit.d.ts -o -name jsconfig.json -o -name '*.test.js' \) -delete
      rm -rf "$dst"; mv "$dst.tmp.$$" "$dst"
    else
      mkdir -p "$(dirname "$dst")"; cp "$src" "$dst.tmp.$$"; mv -f "$dst.tmp.$$" "$dst"
    fi
    echo "$dst" >> "$manifest.new"
  done < <(sources)
  write_local_json
}

check_mods() {
  local d failed=0
  for d in "$repo"/app/mods/*/; do
    herdr-app check "$app_home/mods/$(basename "$d")" || failed=1
  done
  return $failed
}

remove_recorded() {
  local path
  [[ -f "$manifest" ]] || return 0
  while read -r path; do
    case "$path" in
      "$app_home"/mods/agent-usage*|"$app_home"/layouts/agent-usage*|"$bin") rm -rf "$path" ;;
      *) echo "  skipping unexpected manifest entry $path" >&2 ;;
    esac
  done < "$manifest"
  rm -f "$manifest"
}

leave_layout() {
  # Don't pull a layout out from under the window: go back to main first.
  if herdr-app status 2>/dev/null | grep -q '"layout": "agent-usage'; then
    herdr-app "do" layout.switch '{"name":"main"}' >/dev/null || true
  fi
}

cmd_install() {
  preflight
  mkdir -p "$config_dir"
  rm -f "$manifest.new"
  install_binary
  seed_config
  copy_app
  mv -f "$manifest.new" "$manifest"
  if ! check_mods; then
    say "herdr-app check failed; removing what was installed"
    leave_layout; remove_recorded
    exit 1
  fi
  say "installed (recorded in $manifest):"
  sed 's/^/  /' "$manifest"
  say "switch to it: herdr-app do layout.switch '{\"name\":\"agent-usage\"}'"
  say "back to stock: herdr-app do layout.switch '{\"name\":\"main\"}'"
}

cmd_uninstall() {
  [[ -f "$manifest" ]] || die "nothing recorded in $manifest"
  leave_layout
  remove_recorded
  say "removed. $config_dir/config.toml is kept (your accounts)."
}

cmd_status() {
  echo "herdr-app home: $app_home"
  echo "binary:         $bin"
  echo "config:         $config_dir/config.toml"
  if [[ -f "$manifest" ]]; then echo "installed:"; sed 's/^/  /' "$manifest"; else echo "installed:      nothing"; fi
}

case "${1:-}" in
  install) cmd_install ;;
  uninstall) cmd_uninstall ;;
  status) cmd_status ;;
  *) echo "usage: $0 install|uninstall|status" >&2; exit 2 ;;
esac
