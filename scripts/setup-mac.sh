#!/usr/bin/env bash
# Install px0 as a macOS login service: the workbench runs in the background
# on one port, starts at login, and restarts if it crashes. Each repo path
# given is pinned in the workbench. Run it again after a pull to rebuild and
# restart; repos pinned before stay pinned.
#
#   ./scripts/setup-mac.sh [repo ...]     install or update
#   ./scripts/setup-mac.sh --uninstall    stop and remove the service
#
# Environment:
#   PX0_PORT   port to serve on (default 7777)
#   PX0_LABEL  launchd label (default com.px0.workbench)
set -euo pipefail

REPO_DIR=$(cd "$(dirname "$0")/.." && pwd)
PORT=${PX0_PORT:-7777}
LABEL=${PX0_LABEL:-com.px0.workbench}
BIN="$HOME/.local/bin/px0"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
LOG="$HOME/.px0/$LABEL.log"
URL="http://127.0.0.1:$PORT"
DOMAIN="gui/$(id -u)"

if [ "$(uname)" != "Darwin" ]; then
    echo "This script is for macOS only." >&2
    exit 1
fi

stop_service() {
    launchctl bootout "$DOMAIN/$LABEL" 2>/dev/null || true
}

if [ "${1:-}" = "--uninstall" ]; then
    stop_service
    rm -f "$PLIST"
    echo "Removed the $LABEL service. The px0 binary stays at $BIN."
    exit 0
fi

echo "==> Building px0 from $REPO_DIR"
INSTALL_PATH="$BIN" make -C "$REPO_DIR" install >/dev/null
if ! strings "$BIN" | grep -c api/workspaces/switch >/dev/null; then
    echo "The build has no repo switching. Check out the explore_px0_features branch and run again." >&2
    exit 1
fi

echo "==> Stopping any px0 workbench that runs now"
stop_service
# A workbench started by hand holds the single-instance lock and the port.
if [ "$PORT" = "7777" ]; then
    pkill -x px0 2>/dev/null || true
fi
for _ in $(seq 1 20); do
    lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1 || break
    sleep 0.5
done

echo "==> Writing $PLIST"
mkdir -p "$(dirname "$PLIST")" "$(dirname "$LOG")"
# launchd starts jobs with a bare PATH. Keep this shell's PATH, so px0 finds
# git, language servers and coding agents (claude, etc.).
EXTRA_ENV=""
if [ -n "${XDG_STATE_HOME:-}" ]; then
    EXTRA_ENV="<key>XDG_STATE_HOME</key><string>$XDG_STATE_HOME</string>"
fi
cat >"$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key><string>$LABEL</string>
    <key>ProgramArguments</key>
    <array>
        <string>$BIN</string>
        <string>-no-update</string>
        <string>-no-open</string>
        <string>-no-color</string>
        <string>-port</string>
        <string>$PORT</string>
    </array>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key><string>$PATH</string>
        <key>HOME</key><string>$HOME</string>
        $EXTRA_ENV
    </dict>
    <key>WorkingDirectory</key><string>$HOME</string>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><true/>
    <key>StandardOutPath</key><string>$LOG</string>
    <key>StandardErrorPath</key><string>$LOG</string>
</dict>
</plist>
EOF

echo "==> Starting the service"
launchctl bootstrap "$DOMAIN" "$PLIST"
for _ in $(seq 1 40); do
    curl -fs "$URL/api/wb/ping" >/dev/null 2>&1 && break
    sleep 0.5
done
if ! curl -fs "$URL/api/wb/ping" >/dev/null 2>&1; then
    echo "px0 did not start. See $LOG" >&2
    exit 1
fi

post() {
    curl -fs -X POST -H 'Content-Type: application/json' -H "Origin: $URL" -d "$2" "$URL$1"
}

if [ "$#" -gt 0 ]; then
    echo "==> Pinning repos"
fi
for repo in "$@"; do
    if [ ! -d "$repo" ]; then
        echo "    skip $repo (not found)"
        continue
    fi
    id=$(post /api/wb/open "{\"target\":\"$repo\"}" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
    if [ -z "$id" ]; then
        echo "    skip $repo (px0 could not open it)"
        continue
    fi
    post /api/wb/pin "{\"id\":\"$id\",\"pinned\":true}" >/dev/null
    echo "    $repo -> $URL/w/$id/"
done

echo
echo "px0 runs at $URL and starts at login."
echo "Logs: $LOG"
echo "Remove: $REPO_DIR/scripts/setup-mac.sh --uninstall"
if [ -z "${PX0_NO_OPEN:-}" ]; then
    open "$URL"
fi
