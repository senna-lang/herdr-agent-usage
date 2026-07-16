#!/bin/bash
# Claude Code statusLine entry: pass stdin rate_limits JSON to usagebar.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$SCRIPT_DIR/run-usagebar.sh" statusline "$@"
