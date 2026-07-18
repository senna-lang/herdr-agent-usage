#!/bin/bash
# Check GitHub Releases after focus events, at most once per day. The plugin
# manifest is the source of truth for an installed checkout's version because
# locally built binaries intentionally do not receive release ldflags.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
VERSION="$(awk -F '"' '/^version = / { print $2; exit }' "$ROOT/herdr-plugin.toml")"

if [[ -z "$VERSION" ]]; then
  echo "usagebar: could not read version from herdr-plugin.toml" >&2
  exit 1
fi

if [[ "${1:-}" == "--auto" ]]; then
  exec "$SCRIPT_DIR/run-usagebar.sh" check-update --current-version "$VERSION" --quiet
fi

exec "$SCRIPT_DIR/run-usagebar.sh" check-update --current-version "$VERSION" --force
