#!/bin/bash
# Action: open the limits plugin pane as a right split
set -euo pipefail
# HERDR_BIN_PATH can outlive the binary it names: Herdr exports the path of the
# running server binary, and a package manager that rotates version directories
# leaves a dead path behind. `:-` only substitutes on empty, so a stale value
# must be rejected explicitly instead of exec'ing a path that no longer exists.
HERDR_BIN="herdr"
if [ -n "${HERDR_BIN_PATH:-}" ] && [ -x "${HERDR_BIN_PATH}" ]; then
  HERDR_BIN="${HERDR_BIN_PATH}"
fi
exec "$HERDR_BIN" plugin pane open \
  --plugin usagebar \
  --entrypoint limits \
  --placement split \
  --direction right \
  --no-focus
