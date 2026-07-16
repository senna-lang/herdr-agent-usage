#!/bin/bash
# Setup action entry. Auto-builds the usagebar binary on first run when it is
# missing (bin/usagebar is not shipped in the repo). The build lives here —
# a user-initiated, latency-tolerant action — and NOT in run-usagebar.sh,
# which is also the hot path for concurrent event handlers.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

if [[ -z "${USAGEBAR_BIN:-}" && ! -x "$ROOT/bin/usagebar" ]] \
   && ! command -v usagebar >/dev/null 2>&1; then
  if command -v go >/dev/null 2>&1; then
    echo "usagebar: binary not found; building it now (go build)..." >&2
    (cd "$ROOT" && go build -o bin/usagebar ./cmd/usagebar)
  else
    echo "usagebar: binary not found and no Go toolchain available." >&2
    echo "Install Go (https://go.dev/dl/), then run: make build  (in the plugin root)" >&2
    exit 127
  fi
fi

exec "$SCRIPT_DIR/run-usagebar.sh" setup "$@"
