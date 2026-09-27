#!/bin/bash
# Resolve the usagebar binary: prefer sibling build, then PATH.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
# Windows (Git Bash / MSYS) only launches the binary with its .exe suffix;
# keep in step with ensure-binary.sh, which builds it under the same name.
USAGEBAR_EXE="usagebar"
case "$(uname -s)" in
  MINGW* | MSYS* | CYGWIN*) USAGEBAR_EXE="usagebar.exe" ;;
esac

if [[ -n "${USAGEBAR_BIN:-}" && -x "$USAGEBAR_BIN" ]]; then
  exec "$USAGEBAR_BIN" "$@"
fi
if [[ -x "$ROOT/bin/$USAGEBAR_EXE" ]]; then
  exec "$ROOT/bin/$USAGEBAR_EXE" "$@"
fi
if command -v usagebar >/dev/null 2>&1; then
  exec usagebar "$@"
fi
echo "usagebar: binary not found. Run: make build  (or set USAGEBAR_BIN)" >&2
exit 127
