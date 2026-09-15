#!/bin/bash
# Antigravity CLI statusLine entry: pass the stdin session payload to usagebar.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$SCRIPT_DIR/run-usagebar.sh" antigravity-statusline "$@"
