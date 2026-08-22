#!/bin/bash
# Herdr [[startup]] / live-handoff hook: republish sidebar tokens for
# every open agent pane. Server-owned metadata does not survive a cold
# restart; the event path only sees one HERDR_PANE_ID.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$SCRIPT_DIR/run-usagebar.sh" startup "$@"
