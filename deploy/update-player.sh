#!/usr/bin/env bash
# Install the built server and player; leave playback testing to the viewer.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
target="${1:-100.78.192.93:5555}"
apk="player/app/build/outputs/apk/debug/app-debug.apk"
[[ -f projector-remote && -f "$apk" ]] || { echo "Build the server and player APK first." >&2; exit 1; }
adb connect "$target"
adb -s "$target" install -r "$apk"
adb -s "$target" push projector-remote /data/local/tmp/projector-remote.new
adb -s "$target" push deploy/restart-remote.sh /data/local/tmp/projector-remote.restart.sh
adb -s "$target" shell su 0 sh /data/local/tmp/projector-remote.restart.sh
echo "Player installed and server restart requested. Refresh the remote before testing."
