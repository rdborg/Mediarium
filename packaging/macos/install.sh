#!/bin/sh
# Installs the Mediarium release binary as a per-user launchd agent on macOS.
# STATUS: UNTESTED (no Mac was available). Read it before running it.
#
# Run from the folder where you unpacked the release archive:
#   sh install.sh
# Undo with:
#   sh install.sh uninstall
#
# It downloads NOTHING. It only:
#   - warns if 7z or par2 cannot be found (install with: brew install p7zip par2)
#   - copies ./mediarium to ~/.local/share/mediarium/
#   - creates ~/Mediarium/{config,downloads,movies,tv}   (skipped if they exist)
#   - installs the launch agent plist with your home folder filled in
#   - starts it
set -eu

LABEL="io.github.ryanborg.mediarium"
BIN_DIR="$HOME/.local/share/mediarium"
PLIST_DST="$HOME/Library/LaunchAgents/$LABEL.plist"
UID_NUM="$(id -u)"
HERE="$(cd "$(dirname "$0")" && pwd)"

if [ "${1:-}" = "uninstall" ]; then
  launchctl bootout "gui/$UID_NUM/$LABEL" 2>/dev/null || true
  rm -f "$PLIST_DST"
  echo "Removed the launch agent. Your data in ~/Mediarium and the binary in $BIN_DIR were left alone."
  exit 0
fi

if [ ! -f "$HERE/mediarium" ]; then
  echo "mediarium binary not found next to install.sh ($HERE)." >&2
  exit 1
fi
if [ ! -f "$HERE/$LABEL.plist" ]; then
  echo "$LABEL.plist not found next to install.sh ($HERE)." >&2
  exit 1
fi

# Homebrew is not on the default PATH of a non-interactive shell; look there too.
PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"
for tool in 7z par2; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "WARNING: '$tool' not found. Usenet unpacking/repair needs it." >&2
    echo "         Install:  brew install p7zip par2   (see docs/INSTALL.md for the 7z naming caveat)" >&2
  fi
done

mkdir -p "$BIN_DIR" "$HOME/Library/LaunchAgents" "$HOME/Library/Logs/Mediarium"
mkdir -p "$HOME/Mediarium/config" "$HOME/Mediarium/downloads" "$HOME/Mediarium/movies" "$HOME/Mediarium/tv"

cp "$HERE/mediarium" "$BIN_DIR/mediarium"
chmod 755 "$BIN_DIR/mediarium"
# A binary downloaded through a browser carries a quarantine flag and macOS will
# refuse to start it ("cannot verify the developer"). This clears it for this
# one file only. Skip this line if you would rather approve it in System Settings.
xattr -d com.apple.quarantine "$BIN_DIR/mediarium" 2>/dev/null || true

# Fill in the home folder (launchd does not expand ~ or $HOME). The | delimiter
# avoids clashing with slashes; a home path containing | or & would break this.
sed "s|__HOME__|$HOME|g" "$HERE/$LABEL.plist" > "$PLIST_DST"

# Reload if already loaded, then start.
launchctl bootout "gui/$UID_NUM/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$UID_NUM" "$PLIST_DST"

echo "Mediarium is starting. Open http://localhost:8264"
echo "Logs:   ~/Library/Logs/Mediarium/mediarium.log"
echo "Stop:   launchctl bootout gui/$UID_NUM/$LABEL"
echo "Remove: sh install.sh uninstall"
