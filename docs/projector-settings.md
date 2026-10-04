# Projector Picture Settings

Implementation and operational reference for controlling the t950s projector's
picture geometry from the remote. The behavior in this document was discovered
and verified on the physical projector on 2026-10-03.

## What the remote controls

The Picture setup screen exposes:

| Control | Remote range | Device mechanism |
| --- | --- | --- |
| Projected area (zoom) | vendor minimum, currently 40%, through 100% | direct HWC geometry scaling |
| Four-corner mapping | X 0–64, Y 0–36 for each corner | direct HWC geometry writes; global settings persist final values |
| Manual keystone | horizontal and vertical, -40° through 40° | vendor keystone provider |
| Projection position | front, rear, ceiling-front, ceiling-rear | vendor projector-type provider |
| Manual focus | press-and-hold near/far | vendor manual-focus provider |
| Automatic adjustments | autofocus, auto keystone, balance, obstacle avoidance, screen detection, vertical correction | vendor focus-settings provider |

The responsive layout uses one column on phones and two columns on laptop and
desktop widths. The four-corner editor spans both desktop columns.

## Vendor interfaces

The system app `com.jixin.dispatcher` exports the relevant content providers.
The server invokes them locally with Android's `content call`; no accessibility
automation or Settings UI interaction is involved.

| URI | Operations used |
| --- | --- |
| `content://jx.keystone.provider/keystone` | `readCurrentScreenPercentage`, `readMinScreenPercentage`, `angleByHorizontal`, `angleByVertical`, `adjustHorizontal`, `adjustVertical`, `reset` |
| `content://jx.projectortype.provider/projectorType` | `isOrthographicForward`, `isOrthographicRear`, `isHoistingForward`, `isHoistingRear` and their corresponding setters |
| `content://jx.focussettings.provider/focussettings` | `enabled`, `enable`, and `disable`, with a feature key |
| `content://jx.focusmanual.provider/manualfocus` | `ccw`, `cw`, and `stopCWOrCCW` |

Supported automatic-feature keys are `autoFocus`, `trapezoidAuto`,
`autoBalance`, `autoAvoidance`, `autoCurtain`, and `trapezoidTB`.

Provider calls are relatively expensive Android process calls. The controller
allows two concurrent calls, while projector mutations are serialized so two
clients cannot corrupt geometry. Each call has an eight-second deadline. A
shorter three-second deadline proved unsafe under device load: Go terminated
the `content` subprocess and reported the misleading-looking `signal: killed`
error even though Android's low-memory killer was not involved.

## Four-corner geometry

The live hardware geometry is the property
`persist.vendor.hwc.keystone`. It contains eight comma-separated floating-point
coordinates in a 1920×1080 panel coordinate space. The verified vendor order is:

```text
LB.x, LB.y, RB.x, RB.y, RT.x, RT.y, LT.x, LT.y
```

The names above are the vendor's native corner names. They are not always the
same as the corners a viewer sees because rear projection mirrors the image and
ceiling modes invert it.

The OEM Settings app represents each corner as steps rather than raw pixels.
The remote preserves those semantics:

- X is 0–64 and one step is 9.6 panel pixels.
- Y is 0–36 and one step is 10.8 panel pixels.
- Zero means the uncorrected outer edge for that corner.
- Increasing a value moves that visible corner inward.

The raw deltas are intentionally different for each corner:

| Corner | Increasing X | Increasing Y |
| --- | --- | --- |
| LT | `raw[6] += 9.6` | `raw[7] -= 10.8` |
| RT | `raw[4] -= 9.6` | `raw[5] -= 10.8` |
| RB | `raw[2] -= 9.6` | `raw[3] += 10.8` |
| LB | `raw[0] += 9.6` | `raw[1] += 10.8` |

These signs were verified by calling every corresponding OEM nudge operation
and observing the property. Do not normalize them into a single Cartesian rule;
that recreates the inverted-control bug.

The browser maps visible corners to native corners according to projection
position:

| Visible corner | Front | Rear | Ceiling front | Ceiling rear |
| --- | --- | --- | --- | --- |
| LT | LT | RT | RB | LB |
| RT | RT | LT | LB | RB |
| RB | RB | LB | LT | RT |
| LB | LB | RB | RT | LT |

Drag updates are sent at most once every 100 ms with `final: false`. Pointer
release sends one `final: true` update. Every preview updates the HWC property;
only the final update persists the step counters in Android global settings:

```text
jstep_four_point_trapezoid_{lt,rt,rb,lb}{x,y}
```

This separation is important: `settings put global` is too slow to run on every
pointer frame.

## Zoom

Zoom also writes the HWC geometry directly. The server derives the current
projected-area percentage from the geometry's bounding rectangle. To reach a
target percentage it scales all four points around their centroid by:

```text
sqrt(target percentage / current percentage)
```

This keeps the existing corner shape and center rather than resetting manual
mapping. Resulting coordinates are clamped to the 1920×1080 panel. The slider
updates optimistically and sends a throttled command every 100 ms while it is
moving, followed by the final value on release.

## Making changes appear immediately

`setprop` completes quickly, but setting a property does not itself guarantee a
new Android frame. On a static screen the new geometry could be stored while the
old frame remained projected for several seconds. After each geometry write the
server therefore asks SurfaceFlinger to repaint:

```sh
service call SurfaceFlinger 1004
```

Transaction 1004 was tested on Android 13 on this device. It fails with
`Operation not permitted` as the ADB `shell` user and succeeds as root. Run
`projector-remote` as root for real-time zoom and corner feedback. The boot hook
does this already.

## WebSocket state and latency design

The browser sends settings commands through `/ws`. Relevant client messages are:

```json
{"type":"set_zoom","percent":60}
{"type":"set_corner","corner":"lt","x":12,"y":8,"final":false}
{"type":"set_keystone","axis":"horizontal","angle":-3}
{"type":"set_projection_mode","mode":"rear"}
{"type":"set_projection_feature","key":"autoFocus","enabled":true}
{"type":"focus_start","direction":"near"}
{"type":"focus_stop"}
{"type":"reset_keystone"}
```

The server publishes one authoritative `projector_settings` snapshot and then
small `projector_settings_patch` messages. Every settings message carries a
monotonic `revision`; the browser rejects stale snapshots and keeps local
optimistic values until the matching patch arrives. Failed commands produce a
`settings_error`, which rolls the optimistic UI back to confirmed state.

A full OEM settings inventory is slow: the deployed device took 11.161 seconds
in the latest measured cold scan. The server now starts that scan once at boot,
caches the result, and updates the cache after each successful command. A PWA
refresh therefore reuses the warmed snapshot instead of starting another scan.
If a browser connects during the first cold scan immediately after server boot,
it still waits for that one scan.

If the initial inventory fails, the server logs the failed attempt and retries
in the background every two seconds. Connected clients may briefly receive a
`settings_error`, followed automatically by a full `projector_settings`
snapshot when the provider recovers. A transient failure is never retained as
the permanent cache state.

Measured after deployment over the current tailnet path:

- Cached settings arrived after about 180 ms on reconnect.
- Zoom mutations completed on the projector in 84–86 ms.
- WebSocket acknowledgements were observed in 157–362 ms, including network
  and client scheduling time.
- The HWC property contained the new geometry at the first post-command poll.

Each successfully applied projector command is logged with device-side timing:

```text
wsserver: projector command type=set_zoom applied_in=84ms
```

## PWA caching

The generated service worker uses `skipWaiting` and `clientsClaim`. The Go HTTP
server additionally sends:

- `Cache-Control: no-cache` for the app shell, service worker, manifest, and
  other non-fingerprinted paths;
- `Cache-Control: public, max-age=31536000, immutable` for `/assets/*`, whose
  filenames contain content hashes.

This makes a normal refresh revalidate the app shell while retaining efficient
long-term caching for immutable JS, CSS, fonts, and images.

## Diagnostics

Check the server and its settings timings:

```sh
adb shell "ps -A | grep '[p]rojector-remote'"
adb shell 'tail -n 100 /data/local/tmp/projector-remote.log'
```

Read the active geometry without changing it:

```sh
adb shell getprop persist.vendor.hwc.keystone
```

Check that the app shell is revalidated and fingerprinted assets are immutable:

```sh
curl -I http://PROJECTOR_IP:8080/
curl -I http://PROJECTOR_IP:8080/assets/ASSET_HASH.js
```

When testing a geometry change manually, always save the exact property string
first and restore it afterward. A percentage-only restore may not reproduce a
manually mapped quadrilateral exactly.

## Verification

Run the focused and full checks before deployment:

```sh
go test ./...
go test -race ./internal/wsserver ./internal/deviceio
npm --prefix web run lint
npm --prefix web run build
```

The lint command currently reports existing advisory React warnings but exits
successfully. The Go tests, race detector, and production PWA build must pass.

For a safe deployment, upload to a different filename, compare checksums, keep
the existing binary as a backup, then rename and restart. Never replace the
active path until the upload has completed:

```sh
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 \
  go build -trimpath -o projector-remote ./cmd/server
sha256sum projector-remote
adb push projector-remote /data/local/tmp/projector-remote.new
adb shell 'chmod 755 /data/local/tmp/projector-remote.new && sha256sum /data/local/tmp/projector-remote.new'
adb shell 'if [ -e /data/local/tmp/projector-remote ]; then cp /data/local/tmp/projector-remote /data/local/tmp/projector-remote.bak; fi; mv /data/local/tmp/projector-remote.new /data/local/tmp/projector-remote'
adb shell "ps -A | grep '[p]rojector-remote'"
adb shell 'su 0 kill OLD_PID_FROM_PREVIOUS_COMMAND' # skip on first install
adb shell "su 0 sh -c 'nohup /data/local/tmp/projector-remote -listen :8080 > /data/local/tmp/projector-remote.log 2>&1 &'"
curl -fsS http://PROJECTOR_IP:8080/healthz
```

Also verify that the new process is root-owned and that the settings-cache log
appears. Keep the backup until zoom and one corner update have both been tested.

The implementation lives primarily in:

- `internal/deviceio/projector.go` — vendor providers, geometry, persistence,
  repaint, focus watchdog;
- `internal/wsserver/hub.go` — warmed snapshot, revisions, patches, timings;
- `internal/wsserver/protocol.go` — incoming command validation/dispatch;
- `web/src/settings/SettingsPage.tsx` — responsive controls and orientation;
- `web/src/ws/WsContext.tsx` — optimistic state and stale-response handling.
