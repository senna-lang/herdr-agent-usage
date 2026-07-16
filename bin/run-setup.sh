#!/bin/bash
# Setup action entry. Resolves the usagebar binary on first run when it is
# missing (bin/usagebar is not shipped in the repo): build with the local Go
# toolchain, else download a prebuilt release binary. The resolution lives
# here — a user-initiated, latency-tolerant action — and NOT in
# run-usagebar.sh, which is also the hot path for concurrent event handlers.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

REPO="senna-lang/herdr-agent-usage"

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

download_release_binary() {
  local asset dest tmp
  asset="$(release_asset_name)" || return 1
  dest="$ROOT/bin/usagebar"
  tmp="$dest.download"

  # gh works for private and public repos (uses the user's auth);
  # curl covers public repos without gh installed.
  if command -v gh >/dev/null 2>&1; then
    echo "usagebar: downloading prebuilt binary ($asset) via gh..." >&2
    gh release download --repo "$REPO" --pattern "$asset" -O "$tmp" 2>/dev/null || rm -f "$tmp"
  fi
  if [[ ! -s "$tmp" ]] && command -v curl >/dev/null 2>&1; then
    echo "usagebar: downloading prebuilt binary ($asset) via curl..." >&2
    curl -fsSL "https://github.com/$REPO/releases/latest/download/$asset" -o "$tmp" || rm -f "$tmp"
  fi
  [[ -s "$tmp" ]] || return 1
  chmod +x "$tmp"
  # Sanity check before installing: the binary must run on this machine.
  "$tmp" version >/dev/null 2>&1 || { rm -f "$tmp"; return 1; }
  mv -f "$tmp" "$dest"
}

if [[ -z "${USAGEBAR_BIN:-}" && ! -x "$ROOT/bin/usagebar" ]] \
   && ! command -v usagebar >/dev/null 2>&1; then
  if command -v go >/dev/null 2>&1; then
    echo "usagebar: binary not found; building it now (go build)..." >&2
    (cd "$ROOT" && go build -o bin/usagebar ./cmd/usagebar)
  elif download_release_binary; then
    echo "usagebar: prebuilt binary installed to bin/usagebar" >&2
  else
    echo "usagebar: binary not found; no Go toolchain and the prebuilt download failed." >&2
    echo "Fix one of:" >&2
    echo "  - install Go (https://go.dev/dl/), then run: make build  (in the plugin root)" >&2
    echo "  - install gh (https://cli.github.com/) and authenticate, then re-run setup" >&2
    exit 127
  fi
fi

exec "$SCRIPT_DIR/run-usagebar.sh" setup "$@"
