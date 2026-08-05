#!/bin/bash
# Setup action entry. Resolves the usagebar binary on first run when it is
# missing — this is a fallback for installs that predate the
# herdr-plugin.toml [[build]] hook (see bin/ensure-binary.sh), or that ran
# it offline with no Go toolchain. The resolution lives in a separate
# script — a user-initiated, latency-tolerant path — and NOT in
# run-usagebar.sh, which is also the hot path for concurrent event handlers.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

"$SCRIPT_DIR/ensure-binary.sh"

exec "$SCRIPT_DIR/run-usagebar.sh" setup "$@"
