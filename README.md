# Projector Remote

A low-latency remote control for an Android-TV-based projector, controlled
over ADB from a browser PWA on the local network.

See `docs/superpowers/specs/2026-09-08-projector-remote-design.md` for the
full design.

## Prerequisites

- Go 1.22+
- Node.js / npm
- `adb` installed and already paired/authorized with the projector
  (`adb connect <projector-ip>:<port>`; confirm with `adb devices -l`)

## Build

```bash
go generate ./...          # builds the React frontend into web/dist
go build -o projector-remote ./cmd/server
```

## Run

```bash
./projector-remote -listen :8080 -target <projector-ip>:<port>
```

Then, from any device on the same private network, open
`http://<this-machine-ip>:8080` in a browser. On mobile, use
"Add to Home Screen" to install it as a standalone app.

## Notes

- No authentication — anyone on the LAN with the URL can control the
  projector (intentional, see design doc).
- Text input is ASCII-only.
- If the projector's IP or ADB port ever changes, restart the server with
  an updated `-target`.
