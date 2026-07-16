#!/bin/bash
# Resolve the usagebar binary: prefer sibling build, then PATH.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

if [[ -n "${USAGEBAR_BIN:-}" && -x "$USAGEBAR_BIN" ]]; then
  exec "$USAGEBAR_BIN" "$@"
fi
if [[ -x "$ROOT/bin/usagebar" ]]; then
  exec "$ROOT/bin/usagebar" "$@"
fi
if command -v usagebar >/dev/null 2>&1; then
  exec usagebar "$@"
fi
echo "usagebar: binary not found. Run: make build  (or set USAGEBAR_BIN)" >&2
exit 127
