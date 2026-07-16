#!/bin/bash
# Action: open the limits plugin pane as a right split
set -euo pipefail
HERDR_BIN="${HERDR_BIN_PATH:-herdr}"
exec "$HERDR_BIN" plugin pane open \
  --plugin usagebar \
  --entrypoint limits \
  --placement split \
  --direction right \
  --no-focus
