# Projector Remote

A low-latency remote control for an Android-TV-based projector: a
Go server that runs directly on the projector, serving a browser PWA
and driving the device locally (uinput key injection, direct pointer-
node writes, local `input`/`am` for text and app launch).

See `docs/superpowers/specs/2026-09-08-on-device-server-design.md` for
the full design, and `docs/device-guide.md` for the operational
reference on cross-compiling, deploying, and boot-starting Go binaries
on this specific device.

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

## Deploy and run (on the projector)

```bash
adb push projector-remote /data/local/tmp/projector-remote
adb shell chmod 755 /data/local/tmp/projector-remote
adb shell "nohup /data/local/tmp/projector-remote -listen :8080 > /data/local/tmp/projector-remote.log 2>&1 &"
```

To start automatically on boot, see `docs/device-guide.md` §5 (the
`/data/local/tmp/startup.sh` hook).

Then, from any device on the same private network, open
`http://<projector-ip>:8080` in a browser. On mobile, use
"Add to Home Screen" to install it as a standalone app.

The projector's IP changes on every power cycle (DHCP) unless you set
a static DHCP reservation for it on your router — recommended, since
there's no runtime discovery mechanism.

## Notes

- No authentication — anyone on the LAN with the URL can control the
  projector (intentional, see design doc).
- Text input is ASCII-only.
- `/dev/uinput` and the pointer input node must be accessible to
  whatever user runs the server (confirmed world-writable / group-
  accessible to `shell` on this device — see `docs/device-guide.md`).
