# Vandana Cinema home

A standalone Android launcher in `player/home/`, package
`com.projectorremote.home`. It shares the Gradle project with the player but
produces a separate APK. Installing or removing it does not update the player.

## Behavior

- Morning (05:00–11:59), afternoon (12:00–16:59), or evening greeting for
  Vandana, plus “Your little cinema awaits.”
- Offline moon-and-mountain artwork, clock, and local date. No external image
  requests, video backgrounds, or continuous animation.
- A QR code for `http://<current-address>:8080`. The app checks every five
  seconds while visible, preferring Wi-Fi, then Ethernet, then `tailscale0`.
  A Tailscale-only connection shows a different phone connection instruction.
- A local `/healthz` probe reports remote readiness. Disconnected and server
  startup states retry automatically. The original home and network buttons
  work independently of the remote server.
- **Open original home** explicitly opens the OEM activity. **Network settings**
  opens Android Wi-Fi settings, with general settings as a fallback.
- Home returns to this screen. The phone remote also has **Apps → Open original
  home**, using the existing launch message and server implementation.
- The home activity keeps the display awake while in front, so the OEM
  screensaver does not hide the QR code. Other activities retain their normal
  sleep behavior. No global screensaver settings are changed.

The composition scales from a 1280×720 landscape stage, retaining aspect ratio
with letterboxing on other outputs. Content has TV safe margins. Controls use
native Android buttons, explicit D-pad focus links, and a gold focus state.
Greeting text supports system font enlargement through 125%; this is a dedicated
landscape TV surface. English copy lives in `home/src/main/res/values/strings.xml`.

## Device findings — 2026-10-08

- Tailscale ADB: `100.78.192.93:5555`, Android 13.
- Original home: `com.jixin.launcher5/.MainActivity`.
- Display: physical 1920×1080, override 1280×720; density override 200.
- LAN address at inspection: `192.168.122.102` (not saved in the app).
- Time zone: `Asia/Kolkata`.
- OEM dream: `com.jixin.screensaver/.DeskClockDreamService`.

The standard `cmd package set-home-activity` command alone does not work on this
firmware. Its `services.jar` implementation of
`ComputerEngine.applyPostResolutionFilter` filters HOME candidates using
`persist.jx.launcher.highpriority`. The OEM APK's `MainActivity.onResume`
unconditionally resets this property to `com.jixin.launcher5` whenever opened.
Both behaviors were confirmed from the device's own DEX code and live testing.

The root helper `deploy/projector-home-up.sh` restores the chosen Home target
once per second when necessary. Corrections only change the target: they do not
interrupt the original UI, settings, or playback. The boot hook starts the helper
with `--show-home` to show the welcome screen once at startup. Remove
`/data/local/tmp/projector-home.enabled` to stop enforcement.

The existing writable `/data/local/tmp/startup.sh` now starts this helper in
addition to the remote server, DNS relay, and Tailscale. No `/system` or `/vendor`
changes are required. A first-launch crash was also fixed by requesting the
window controller after installing the activity's content view.

## Build locally

From `player/`, with JDK 21 and the configured Android SDK:

```bash
./gradlew :home:assembleDebug :home:testDebugUnitTest :home:lintDebug
```

APK: `player/home/build/outputs/apk/debug/home-debug.apk`.

For the phone's original-home shortcut, build the frontend and embedded server
from the repository root:

```bash
npm --prefix web run lint
npm --prefix web run build
go test ./...
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 \
  go build -trimpath -o projector-remote ./cmd/server
sh -n deploy/projector-home-up.sh deploy/startup.sh
```

The server build includes the current working tree, including pre-existing media
library changes. Use the atomic backup/upload procedure in the README. The
standalone welcome APK can be installed without replacing the running server.

## Deployment

Deploy only when requested and playback can be interrupted. On first deployment,
record the currently resolved home and preserve the original property/startup
script. Do not overwrite these backups on subsequent updates.

```bash
adb -s 100.78.192.93:5555 shell cmd package resolve-activity --brief \
  -a android.intent.action.MAIN -c android.intent.category.HOME
adb -s 100.78.192.93:5555 shell \
  'getprop persist.jx.launcher.highpriority > /data/local/tmp/projector-home-original-priority.txt'
adb -s 100.78.192.93:5555 shell su 0 cp \
  /data/local/tmp/startup.sh /data/local/tmp/startup.pre-home-20261008.sh
adb -s 100.78.192.93:5555 install -r \
  player/home/build/outputs/apk/debug/home-debug.apk
adb -s 100.78.192.93:5555 shell input keyevent KEYCODE_WAKEUP
adb -s 100.78.192.93:5555 shell am start \
  -n com.projectorremote.home/.HomeActivity
adb -s 100.78.192.93:5555 push deploy/projector-home-up.sh \
  /data/local/tmp/projector-home-up.sh
adb -s 100.78.192.93:5555 push deploy/startup.sh /data/local/tmp/startup.sh
adb -s 100.78.192.93:5555 shell \
  'chmod 755 /data/local/tmp/projector-home-up.sh /data/local/tmp/startup.sh'
adb -s 100.78.192.93:5555 shell su 0 touch /data/local/tmp/projector-home.enabled
adb -s 100.78.192.93:5555 shell \
  "su 0 sh -c 'nohup /data/local/tmp/projector-home-up.sh > /data/local/tmp/projector-home.log 2>&1 &'"
adb -s 100.78.192.93:5555 shell cmd package set-home-activity --user 0 \
  com.projectorremote.home/.HomeActivity
adb -s 100.78.192.93:5555 shell input keyevent KEYCODE_HOME
```

If the live startup script differs, merge only the guard stanza instead of
replacing the file. The guard's PID file makes repeated manual starts idempotent.

Verify the resolved HOME component, the OEM exit button, Home return, the phone
shortcut after the server update, network settings, player launch/exit, and a
real reboot. Confirm the QR address after DHCP changes. Review the projected
layout manually for greeting legibility, focus contrast, text clipping, and QR
scanning from the phone. Build checks and UI hierarchy inspection do not
establish visual quality or phone-camera scanning.

## Rollback

Stop enforcement first, restore the original priority, then restore the original
launcher before removing the welcome app:

```bash
adb -s 100.78.192.93:5555 shell su 0 rm -f /data/local/tmp/projector-home.enabled
# Allow the guard to exit before restoring the property.
adb -s 100.78.192.93:5555 shell sleep 2
adb -s 100.78.192.93:5555 shell su 0 setprop \
  persist.jx.launcher.highpriority com.jixin.launcher5
adb -s 100.78.192.93:5555 shell cmd package set-home-activity --user 0 \
  com.jixin.launcher5/.MainActivity
adb -s 100.78.192.93:5555 shell input keyevent KEYCODE_HOME
adb -s 100.78.192.93:5555 uninstall com.projectorremote.home
```

The OEM launcher stays enabled and installed throughout. The guard stanza can
remain in `startup.sh` when disabled: the helper exits without its enable marker.
Alternatively remove just that stanza, preserving the other boot services.

The deployment retains these on-device rollback references:

- `/data/local/tmp/projector-remote.pre-home-20261008`
- `/data/local/tmp/projector-remote-state.pre-home-20261008.json`
- `/data/local/tmp/startup.pre-home-20261008.sh`
- `/data/local/tmp/projector-home-original-priority.txt`
