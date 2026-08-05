#!/bin/bash
# Provision the usagebar binary. Three modes, because the callers ask three
# different questions:
#
#   --in-tree  Guarantee THIS checkout carries bin/usagebar. Used by the
#              herdr-plugin.toml [[build]] hook, which runs on every
#              `herdr plugin install` — including reinstall/update, the only
#              way to update — against a fresh checkout that never carries the
#              gitignored binary. $USAGEBAR_BIN and a usagebar on PATH
#              deliberately do NOT satisfy this mode: they come from the
#              install-time shell, which need not match the Herdr server
#              environment that later runs the event hooks. Treating them as
#              satisfying would reintroduce issue #48 silently.
#
#   --build    Always compile from source, and fail hard. Used by `make build`,
#              where falling back to a prebuilt release would mask a broken
#              tree instead of reporting it.
#
#   (default)  Ensure usagebar is resolvable the way run-usagebar.sh resolves
#              it: $USAGEBAR_BIN -> sibling bin/usagebar -> PATH. Used by
#              run-setup.sh as the fallback for installs predating the build
#              hook.
#
# Build-or-download modes compile with the local Go toolchain and fall back to
# a prebuilt release binary.
# See https://github.com/senna-lang/herdr-agent-usage/issues/48
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

REPO="senna-lang/herdr-agent-usage"
# The manifest is the source of truth for a checkout's version: locally built
# binaries intentionally do not receive release ldflags (see
# bin/run-update-check.sh).
VERSION="$(awk -F '"' '/^version = / { print $2; exit }' "$ROOT/herdr-plugin.toml")"

# Normalize uname output to the Go GOOS/GOARCH names used by release assets
# (.github/workflows/release.yml): usagebar-<os>-<arch>.
release_asset_name() {
  local os arch
  case "$(uname -s)" in
    Darwin) os="darwin" ;;
    Linux) os="linux" ;;
    *) return 1 ;;
  esac
  case "$(uname -m)" in
    x86_64) arch="amd64" ;;
    arm64 | aarch64) arch="arm64" ;;
    *) return 1 ;;
  esac
  echo "usagebar-${os}-${arch}"
}

# Fetch $asset from release $tag ("" = latest) into $tmp, leaving no partial
# file behind on failure. gh works for private and public repos (it uses the
# user's auth); curl covers public repos without gh installed.
fetch_release_asset() {
  local tag="$1" asset="$2" tmp="$3"
  local gh_args=(release download) curl_path
  if [[ -n "$tag" ]]; then
    gh_args+=("$tag")
    curl_path="download/$tag"
  else
    curl_path="latest/download"
  fi
  gh_args+=(--repo "$REPO" --pattern "$asset" -O "$tmp")

  if command -v gh >/dev/null 2>&1; then
    gh "${gh_args[@]}" 2>/dev/null || rm -f "$tmp"
  fi
  if [[ ! -s "$tmp" ]] && command -v curl >/dev/null 2>&1; then
    curl -fsSL "https://github.com/$REPO/releases/$curl_path/$asset" -o "$tmp" || rm -f "$tmp"
  fi
  [[ -s "$tmp" ]]
}

# Install a prebuilt binary into bin/usagebar. Prefers the release matching
# this checkout's manifest version — `herdr plugin install --ref` can install
# source that is older or newer than the latest release — and falls back to
# latest when that tag carries no assets (an unreleased main, say).
download_release_binary() {
  local asset dest tmp
  asset="$(release_asset_name)" || return 1
  dest="$ROOT/bin/usagebar"
  tmp="$dest.download"
  rm -f "$tmp"

  echo "usagebar: downloading prebuilt binary ($asset)..." >&2
  # The version-matched attempt is speculative — a 404 here just means this
  # checkout is ahead of the last release — so keep its noise off stderr.
  if [[ -n "$VERSION" ]]; then
    fetch_release_asset "v$VERSION" "$asset" "$tmp" 2>/dev/null || true
  fi
  if [[ ! -s "$tmp" ]] && ! fetch_release_asset "" "$asset" "$tmp"; then
    return 1
  fi

  chmod +x "$tmp"
  # Sanity check before installing: the binary must run on this machine.
  "$tmp" version >/dev/null 2>&1 || { rm -f "$tmp"; return 1; }
  mv -f "$tmp" "$dest"
}

build_from_source() {
  command -v go >/dev/null 2>&1 || return 1
  echo "usagebar: building bin/usagebar (go build)..." >&2
  mkdir -p "$ROOT/bin"
  (cd "$ROOT" && go build -o bin/usagebar ./cmd/usagebar)
}

# Guarantee $ROOT/bin/usagebar exists, by any available means. A source build
# failing (cold module cache, no network, no toolchain) is not fatal on its
# own — the prebuilt download is a real second chance.
install_binary() {
  if build_from_source; then
    return 0
  fi
  if command -v go >/dev/null 2>&1; then
    echo "usagebar: go build failed; trying the prebuilt release binary..." >&2
  fi
  if download_release_binary; then
    echo "usagebar: prebuilt binary installed to bin/usagebar" >&2
    return 0
  fi
  echo "usagebar: could not build or download the usagebar binary." >&2
  echo "Fix one of:" >&2
  echo "  - install Go (https://go.dev/dl/), then run: make build  (in the plugin root)" >&2
  echo "  - install gh (https://cli.github.com/) and authenticate, then re-run setup" >&2
  return 127
}

# True when usagebar is already resolvable the way run-usagebar.sh resolves it.
usagebar_resolvable() {
  [[ -n "${USAGEBAR_BIN:-}" && -x "$USAGEBAR_BIN" ]] && return 0
  [[ -x "$ROOT/bin/usagebar" ]] && return 0
  command -v usagebar >/dev/null 2>&1
}

case "${1:-}" in
  --in-tree)
    if [[ ! -x "$ROOT/bin/usagebar" ]]; then
      install_binary
    fi
    ;;
  --build)
    build_from_source || {
      echo "usagebar: go build failed (is the Go toolchain installed?)" >&2
      exit 1
    }
    ;;
  "")
    if ! usagebar_resolvable; then
      install_binary
    fi
    ;;
  *)
    echo "usage: ensure-binary.sh [--in-tree|--build]" >&2
    exit 2
    ;;
esac
