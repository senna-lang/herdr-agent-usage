#!/bin/bash
# Open an existing isolated Herdr session in Ghostty for a human-maintained README screenshot.
set -euo pipefail

usage() {
  cat <<'EOF'
usage: scripts/open-readme-screenshot.sh --session NAME [--home DIR] [--herdr PATH]

Open NAME in a new Ghostty window. The session must already be running and
contain the prepared screenshot fixture. This script does not capture or write
an image; the maintainer takes and reviews the screenshot manually.
EOF
}

session=""
home="${HOME}"
herdr_bin="${HERDR_BIN:-herdr}"

while (($#)); do
  case "$1" in
    --session)
      session="${2:-}"
      shift 2
      ;;
    --home)
      home="${2:-}"
      shift 2
      ;;
    --herdr)
      herdr_bin="${2:-}"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$session" ]]; then
  usage >&2
  exit 2
fi
if [[ "$(uname)" != "Darwin" ]]; then
  echo "README screenshot launcher requires macOS and Ghostty." >&2
  exit 1
fi
if ! command -v "$herdr_bin" >/dev/null 2>&1; then
  echo "herdr executable not found: $herdr_bin" >&2
  exit 1
fi
if [[ "$(osascript -e 'tell application "System Events" to tell process "Ghostty" to get exists')" != "true" ]]; then
  echo "Open Ghostty first, then rerun this command." >&2
  exit 1
fi

config_home="${XDG_CONFIG_HOME:-$home/.config}"
state_home="${XDG_STATE_HOME:-$home/.local/state}"
data_home="${XDG_DATA_HOME:-$home/.local/share}"
cache_home="${XDG_CACHE_HOME:-$home/.cache}"
socket_path="$config_home/herdr/sessions/$session/herdr.sock"

if ! HERDR_ENV=1 HOME="$home" XDG_CONFIG_HOME="$config_home" XDG_STATE_HOME="$state_home" XDG_DATA_HOME="$data_home" XDG_CACHE_HOME="$cache_home" "$herdr_bin" --session "$session" pane layout >/dev/null; then
  echo "Herdr session is unavailable: $session" >&2
  exit 1
fi

attach_script="$(mktemp -t herdr-readme-screenshot.XXXXXX)"
trap 'rm -f "$attach_script"' EXIT
cat >"$attach_script" <<EOF
#!/bin/zsh
unset HERDR_ENV HERDR_SESSION HERDR_SOCKET_PATH HERDR_CLIENT_SOCKET_PATH HERDR_PANE_ID HERDR_TAB_ID HERDR_WORKSPACE_ID HERDR_BIN_PATH
export HOME=$(printf '%q' "$home")
export XDG_CONFIG_HOME=$(printf '%q' "$config_home")
export XDG_STATE_HOME=$(printf '%q' "$state_home")
export XDG_DATA_HOME=$(printf '%q' "$data_home")
export XDG_CACHE_HOME=$(printf '%q' "$cache_home")
export HERDR_SOCKET_PATH=$(printf '%q' "$socket_path")
exec $(printf '%q' "$herdr_bin") --session $(printf '%q' "$session")
EOF
chmod 700 "$attach_script"

osascript - "$attach_script" <<'APPLESCRIPT'
on run argv
  set attachCommand to "exec " & quoted form of item 1 of argv
  tell application "System Events"
    tell process "Ghostty"
      set frontmost to true
      keystroke "n" using command down
    end tell
    delay 1
    click at {800, 500}
    set savedClipboard to the clipboard
    set the clipboard to attachCommand
    tell process "Ghostty" to keystroke "v" using command down
    key code 36
    delay 1
    set the clipboard to savedClipboard
  end tell
end run
APPLESCRIPT

# The command has been handed to Ghostty; its shell no longer needs this file.
trap - EXIT
rm -f "$attach_script"
echo "Opened Ghostty for Herdr session: $session"
