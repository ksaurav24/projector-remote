#!/system/bin/sh
# OEM MainActivity.onResume unconditionally reclaims this property.
# Keep Home pointed at our launcher without interrupting the OEM UI.
# Remove projector-home.enabled to stop enforcement before rollback.
BASE=/data/local/tmp
HOME_PACKAGE=com.projectorremote.home
PID_FILE=$BASE/projector-home-guard.pid

[ -f "$BASE/projector-home.enabled" ] || exit 0

# The startup hook runs once per boot, but make manual starts idempotent too.
if [ -f "$PID_FILE" ]; then
    previous_pid=$(cat "$PID_FILE")
    case "$previous_pid" in
        ''|*[!0-9]*) ;;
        *)
            if [ -r "/proc/$previous_pid/cmdline" ]; then
                case "$(tr '\000' ' ' < "/proc/$previous_pid/cmdline")" in
                    *'/data/local/tmp/projector-home-up.sh'*) exit 0 ;;
                esac
            fi
            ;;
    esac
fi

echo $$ > "$PID_FILE"
trap 'rm -f "$PID_FILE"' EXIT
trap 'exit 0' TERM INT

pm path "$HOME_PACKAGE" >/dev/null 2>&1 || exit 0
setprop persist.jx.launcher.highpriority "$HOME_PACKAGE" || exit 1

# Boot startup should show the welcome screen once. Later corrections only
# change the Home target, leaving settings, playback, and the OEM UI in front.
if [ "$1" = '--show-home' ]; then
    am start -n "$HOME_PACKAGE/.HomeActivity"
fi

while [ -f "$BASE/projector-home.enabled" ]; do
    if [ "$(getprop persist.jx.launcher.highpriority)" != "$HOME_PACKAGE" ]; then
        pm path "$HOME_PACKAGE" >/dev/null 2>&1 || exit 0
        setprop persist.jx.launcher.highpriority "$HOME_PACKAGE" || exit 1
        echo 'Restored custom Home target after OEM launcher resumed.'
    fi
    sleep 1
done
