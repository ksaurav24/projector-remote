# Projector Remote

A low-latency remote control for an Android-TV-based projector: a
Go server that runs directly on the projector, serving a browser PWA
and driving the device locally (uinput key injection, direct pointer-
node writes, local `input`/`am` for text and app launch).

The PWA also browses the NetMirror catalog (Netflix, Prime and
Hotstar skins) and plays titles on the projector in its own player
app, with audio, subtitle, and quality switching, resume, continue
watching, and next-episode auto-play, all controlled from the phone.
Anime browsing uses AniList metadata and ordered stream-provider adapters,
with sub/dub editions and automatic source fallback. See
[`docs/anime-module.md`](docs/anime-module.md) for provider configuration,
API details, and the required player update.

**Browse → Library** plays video files from a laptop. A media server
there (`cmd/mediaserver`) indexes a folder of movies and shows, follows
changes to it, and streams HLS to the projector over Tailscale. Files the
projector can decode are copied; everything else is transcoded on the
laptop's GPU. Audio tracks and subtitles are switchable. The tab appears
only while the laptop is reachable. See
[`docs/media-library.md`](docs/media-library.md).

Bookmark any title from its detail page, then open **Browse → Bookmarks**
to find saved movies, shows, and anime across platforms. Bookmarks are stored
on the projector with watch history and shared across remotes. Other devices
refresh them when focused or every 30 seconds while visible. Finishing a
title does not remove its bookmark.

Its responsive Picture setup screen controls zoom, focus, manual
keystone, four-corner mapping, projection orientation, and the
projector's automatic correction features from a phone or laptop.

```
Phone/laptop browser (PWA)
        │  HTTP/WS, LAN
        ▼
Go server — runs ON the projector (cmd/server)
 ├─ HTTP: serves the embedded React build (go:embed web/dist)
 ├─ WS "/ws": JSON message protocol (internal/wsserver)
 ├─ HTTP "/api/catalog/*": normalized NetMirror, anime + laptop library catalog (internal/catalogapi)
 ├─ internal/deviceio: local command execution
 └─ internal/playback: playback session, next episode, resume
        │                        │  WS "/player" (localhost only)
        ▼                        ▼
   Kernel input subsystem     Player app (player/, Media3 ExoPlayer)
   + `input text` / `am`        plays HLS / MP4 + subtitles
```

`docs/netmirror-api.md` covers the NetMirror API,
`docs/projector-settings.md` documents the picture-control hardware
interfaces and real-time WebSocket design, and `docs/device-guide.md`
§8 covers the device quirks playback depends on.

See `docs/device-guide.md` for the operational reference on cross-
compiling, deploying, and boot-starting Go binaries on this specific
device, and `docs/blog-rooting-the-projector.md` for the story of how
the device got to a state where that's possible in the first place
(ADB persistence, partition layout, and an OEM backdoor found along
the way).

(The earlier host-machine/network-ADB architecture is preserved as the
frozen `external-server-v1` git tag; it's no longer maintained.)

## Prerequisites

- Go 1.22+
- Node.js / npm
- `adb`, used only as a deploy tool (push/run), already paired/authorized
  with the projector (`adb connect <projector-ip>:<port>`; confirm with
  `adb devices -l`)

## Build

```bash
go generate ./...          # builds the React frontend into web/dist
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -o projector-remote ./cmd/server
```

The player app (JDK 21; `mise install` inside `player/` sets it up). It
needs the Android SDK (platform 36): point `player/local.properties` at
it with `sdk.dir=/path/to/android-sdk`, or set `ANDROID_HOME`.

```bash
cd player && ./gradlew assembleDebug
```

The laptop media server (runs where the files are; see
[`docs/media-library.md`](docs/media-library.md)):

```bash
go build -o ~/.local/bin/projector-media ./cmd/mediaserver
```

## Deploy and run (on the projector)

```bash
adb push projector-remote /data/local/tmp/projector-remote.new
adb shell 'chmod 755 /data/local/tmp/projector-remote.new'
adb shell 'if [ -e /data/local/tmp/projector-remote ]; then cp /data/local/tmp/projector-remote /data/local/tmp/projector-remote.bak; fi; mv /data/local/tmp/projector-remote.new /data/local/tmp/projector-remote'
adb shell "ps -A | grep '[p]rojector-remote'"  # note the old PID, if any
adb shell 'su 0 kill OLD_PID'                  # skip on first install
adb shell "su 0 sh -c 'nohup /data/local/tmp/projector-remote -listen :8080 > /data/local/tmp/projector-remote.log 2>&1 &'"
curl -fsS http://PROJECTOR_IP:8080/healthz
```

Root is required for immediate SurfaceFlinger redraws after zoom and
four-corner geometry changes. The rest of the remote can run as
`shell`, but picture updates may remain visually stale until Android
composes another frame. See `docs/projector-settings.md`.

The `.new` → active rename keeps the old process and binary intact
until the upload finishes; `.bak` provides a simple rollback. Do not
use `pkill -f` on this device—see `docs/device-guide.md` §4.

Install the player app, which the server launches when you press Play:

```bash
adb install -r player/app/build/outputs/apk/debug/app-debug.apk
```

The optional **Vandana Cinema** home app (`player/home/`) provides a personal
welcome screen, a QR code for the remote, and access to the original launcher.
It builds as a separate APK. See [`docs/projector-home.md`](docs/projector-home.md)
for build instructions, deployment checks, and rollback.

The server makes HTTPS calls to NetMirror, so it needs `dnsrelay`
running for DNS (`docs/device-guide.md` §7). To start everything
automatically on boot, see `docs/device-guide.md` §5 (the
`/data/local/tmp/startup.sh` hook).

Then, from any device on the same private network, open
`http://<projector-ip>:8080` in a browser. On mobile, use
"Add to Home Screen" to install it as a standalone app.

The projector's IP changes on every power cycle (DHCP) unless you set
a static DHCP reservation for it on your router — recommended, since
there's no runtime discovery mechanism.

## Notes

- No authentication — anyone on the LAN with the URL can control the
  projector (intentional: it's a single-household LAN tool, not a
  multi-tenant service).
- Text input is ASCII-only.
- Picture-settings state is warmed once when the server starts. A
  browser opened during that initial vendor scan may wait several
  seconds; later PWA opens and refreshes use the cached snapshot.
- `/dev/uinput` and the pointer input node must be accessible to
  whatever user runs the server (confirmed world-writable / group-
  accessible to `shell` on this device — see `docs/device-guide.md`).

## Test

```bash
go test ./...
cd web && npm install && npm run build
cd player && ./gradlew testDebugUnitTest
```

## License

MIT, see [LICENSE](LICENSE).
