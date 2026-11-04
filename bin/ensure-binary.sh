#!/bin/bash
# Provision the usagebar binary. Three modes, because the callers ask three
# different questions:
#
#   --in-tree  Guarantee THIS checkout carries bin/usagebar. Used by the
#              herdr-plugin.toml [[build]] hook, which runs on every
#              `herdr plugin install`, and on `herdr plugin update` for
#              GitHub-managed installs (Herdr 0.9.4+), against a fresh checkout
#              that never carries the gitignored binary. $USAGEBAR_BIN and a
#              usagebar on PATH deliberately do NOT satisfy this mode: they come
#              from the install-time shell, which need not match the Herdr server
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
# the prebuilt binary of this checkout's exact release. That binary reaches
# bin/usagebar only after its SHA-256 matches the same release's SHA256SUMS.
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

# Fetch $asset from the exact release $tag into $dest, leaving no partial file
# behind on failure. gh works for private and public repos (it uses the user's
# auth); curl covers public repos when gh is absent or fails. An empty tag is
# refused: there is no "latest" fallback anywhere in this script.
fetch_release_asset() {
  local tag="$1" asset="$2" dest="$3"
  [[ -n "$tag" ]] || return 1
  if command -v gh >/dev/null 2>&1; then
    gh release download "$tag" --repo "$REPO" --pattern "$asset" -O "$dest" 2>/dev/null || rm -f "$dest"
  fi
  if [[ ! -s "$dest" ]] && command -v curl >/dev/null 2>&1; then
    curl -fsSL "https://github.com/$REPO/releases/download/$tag/$asset" -o "$dest" || rm -f "$dest"
  fi
  [[ -s "$dest" ]]
}

# True when $1 is exactly 64 hexadecimal digits.
is_sha256_hex() {
  [[ "$1" =~ ^[0-9A-Fa-f]{64}$ ]]
}

# Name of the SHA-256 tool on PATH. GNU sha256sum is preferred; macOS ships
# shasum, which writes the same record format.
sha256_tool() {
  if command -v sha256sum >/dev/null 2>&1; then
    echo sha256sum
  elif command -v shasum >/dev/null 2>&1; then
    echo shasum
  else
    return 1
  fi
}

# Print the lowercase SHA-256 of $1. Fails when the verifier fails or prints
# anything other than a 64-digit digest, so a broken tool can never match.
sha256_hex() {
  local file="$1" tool out digest
  tool="$(sha256_tool)" || return 1
  if [[ "$tool" == sha256sum ]]; then
    out="$(sha256sum "$file")" || return 1
  else
    out="$(shasum -a 256 "$file")" || return 1
  fi
  digest="${out%% *}"
  is_sha256_hex "$digest" || return 1
  printf '%s\n' "$digest" | tr '[:upper:]' '[:lower:]'
}

# Print the lowercase digest that SHA256SUMS ($1) records for exactly the file
# named $2. Records are "<digest>  <name>" (text mode) or "<digest> *<name>"
# (binary mode). A line is a record for $2 when one whitespace-separated field
# is $2, after an optional "*" marker. Every such line must be exactly one of
# the two forms with a 64-digit digest, and there must be exactly one. Names
# must match exactly, so records for "xusagebar-…", "dist/…" or "…-old" never
# count.
selected_sha256() {
  local sums="$1" name="$2" line padded digest selected found="" count=0
  while IFS= read -r line || [[ -n "$line" ]]; do
    # Fields are matched with spaces on both sides, so a prefix, suffix or path
    # around $name never matches.
    padded=" ${line//[[:space:]]/ } "
    selected=""
    case "$padded" in
      *" $name "* | *" *$name "*) selected=1 ;;
    esac
    [[ -n "$selected" ]] || continue
    count=$((count + 1))
    digest="${line%%[[:space:]]*}"
    if ! is_sha256_hex "$digest" ||
      [[ "$line" != "$digest  $name" && "$line" != "$digest *$name" ]]; then
      echo "usagebar: SHA256SUMS has a malformed record for $name." >&2
      return 1
    fi
    found="$digest"
  done < "$sums"
  if [[ "$count" -ne 1 ]]; then
    echo "usagebar: SHA256SUMS must contain exactly one record for $name (found $count)." >&2
    return 1
  fi
  printf '%s\n' "$found" | tr '[:upper:]' '[:lower:]'
}

# Fetch $asset and SHA256SUMS of release $tag into the staging directory $stage,
# then check the digest. The binary is marked executable and run only after
# the digest matches, so an unverified payload is never executed.
verify_release_binary() {
  local tag="$1" asset="$2" stage="$3" expected actual
  if ! fetch_release_asset "$tag" "$asset" "$stage/$asset"; then
    echo "usagebar: release $tag does not publish $asset." >&2
    return 1
  fi
  if ! fetch_release_asset "$tag" SHA256SUMS "$stage/SHA256SUMS"; then
    echo "usagebar: release $tag does not publish SHA256SUMS to verify $asset." >&2
    return 1
  fi
  expected="$(selected_sha256 "$stage/SHA256SUMS" "$asset")" || return 1
  actual="$(sha256_hex "$stage/$asset")" || {
    echo "usagebar: could not compute the SHA-256 of the downloaded $asset." >&2
    return 1
  }
  if [[ "$actual" != "$expected" ]]; then
    echo "usagebar: $asset from release $tag does not match SHA256SUMS; not installing it." >&2
    return 1
  fi
  if ! chmod +x "$stage/$asset"; then
    return 1
  fi
  # Post-verification sanity check: the verified binary must run here.
  if ! "$stage/$asset" version >/dev/null 2>&1; then
    echo "usagebar: verified $asset does not run on this machine; not installing it." >&2
    return 1
  fi
}

# Install the prebuilt binary of this checkout's exact release. The staging
# directory sits beside the destination, so the final mv is an atomic rename
# within bin/. The subshell's EXIT trap removes the staging directory on every
# path, and the destination changes only after verification succeeds.
download_release_binary() {
  local asset tag stage
  if ! asset="$(release_asset_name)"; then
    echo "usagebar: no prebuilt release binary for this platform." >&2
    return 1
  fi
  if [[ -z "$VERSION" ]]; then
    echo "usagebar: herdr-plugin.toml has no version, so no release can be selected." >&2
    return 1
  fi
  tag="v$VERSION"
  if ! sha256_tool >/dev/null; then
    echo "usagebar: no SHA-256 tool found; install shasum or sha256sum to verify the download." >&2
    return 1
  fi
  mkdir -p "$ROOT/bin" || return 1
  echo "usagebar: downloading prebuilt binary ($asset from $tag)..." >&2
  (
    stage="$(mktemp -d "$ROOT/bin/.usagebar-download.XXXXXX")" || exit 1
    trap 'rm -rf "$stage"' EXIT
    verify_release_binary "$tag" "$asset" "$stage" &&
      mv -f "$stage/$asset" "$ROOT/bin/usagebar"
  )
}

build_from_source() {
  command -v go >/dev/null 2>&1 || return 1
  echo "usagebar: building bin/usagebar with go..." >&2
  mkdir -p "$ROOT/bin" || return 1
  (cd "$ROOT" && go build -o bin/usagebar ./cmd/usagebar)
}

# Go first; the exact-tag prebuilt binary only when the source build fails.
install_binary() {
  if build_from_source; then
    return 0
  fi
  if download_release_binary; then
    echo "usagebar: prebuilt binary installed to bin/usagebar" >&2
    return 0
  fi
  echo "usagebar: could not build or download the usagebar binary." >&2
  echo "Fix one of:" >&2
  echo "  - install Go (https://go.dev/dl/), then run: make build  (in the plugin root)" >&2
  echo "  - install an explicit released ref of this plugin: release v${VERSION:-?} must publish the binary and SHA256SUMS, and they must verify" >&2
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
