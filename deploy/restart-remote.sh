#!/system/bin/sh
set -eu
binary=/data/local/tmp/projector-remote
args=/data/local/tmp/projector-remote.deploy-args
pids=$(pidof projector-remote || true)
set -- $pids
if [ "$#" -gt 1 ]; then
    echo 'More than one server is running; refusing to replace them.' >&2
    exit 1
fi
pid=${1:-}
set -- -listen :8080
if [ -n "$pid" ]; then
    # Retain configured provider endpoints, media-server URLs, and state paths.
    # Read arguments as data, never as shell expressions.
    tr '\000' '\n' < "/proc/$pid/cmdline" > "$args"
    set --
    first=true
    while IFS= read -r arg; do
        if [ "$first" = true ]; then first=false; continue; fi
        set -- "$@" "$arg"
    done < "$args"
fi
chmod 755 "$binary.new"
if [ -e "$binary" ]; then cp "$binary" "$binary.bak"; fi
mv "$binary.new" "$binary"
if [ -n "$pid" ]; then
    kill "$pid"
    attempts=0
    while kill -0 "$pid" 2>/dev/null; do
        attempts=$((attempts + 1))
        if [ "$attempts" -gt 10 ]; then
            echo 'The old server has not stopped; restart was not completed.' >&2
            exit 1
        fi
        sleep 1
    done
fi
nohup "$binary" "$@" > /data/local/tmp/projector-remote.log 2>&1 < /dev/null &
rm -f "$args"
