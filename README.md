# Projector Remote

A low-latency remote control for an Android-TV-based projector: a
Go server that runs directly on the projector, serving a browser PWA
and driving the device locally (uinput key injection, direct pointer-
node writes, local `input`/`am` for text and app launch).

```
Phone/laptop browser (PWA)
        │  HTTP/WS, LAN
        ▼
Go server — runs ON the projector (cmd/server)
 ├─ HTTP: serves the embedded React build (go:embed web/dist)
 ├─ WS "/ws": JSON message protocol (internal/wsserver)
 └─ internal/deviceio: local command execution
        │
        ▼
   Kernel input subsystem (uinput virtual keyboard, mouse evdev node)
   + local exec of `input text` / `am start`
```

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
  projector (intentional: it's a single-household LAN tool, not a
  multi-tenant service).
- Text input is ASCII-only.
- `/dev/uinput` and the pointer input node must be accessible to
  whatever user runs the server (confirmed world-writable / group-
  accessible to `shell` on this device — see `docs/device-guide.md`).

## Test

```bash
go test ./...
cd web && npm install && npm run build
```

## License

MIT, see [LICENSE](LICENSE).
