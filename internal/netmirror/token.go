package netmirror

import (
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// DefaultMMKVPath is where the NetMirror app keeps its key-value store,
// including the usertoken it was issued after passing its OTP screen.
const DefaultMMKVPath = "/data/data/app.netmirror.newtv/files/mmkv/mmkv.default"

var tokenRe = regexp.MustCompile(`[0-9a-fA-F]{32}::[0-9a-fA-F]{32}::[0-9]+::ni`)

// MMKVToken reuses the token the NetMirror app on this device already
// holds, rather than going through NetMirror's OTP flow itself. It
// reads the store fresh on every call, so a token the app refreshes
// is picked up without restarting the server.
type MMKVToken struct {
	Path string
}

func (m MMKVToken) Token(ctx context.Context) (string, error) {
	b, err := os.ReadFile(m.Path)
	if err != nil {
		// The app's data dir is private to its uid. Started from the
		// boot hook the server runs as root and the direct read works;
		// started by hand from `adb shell` it runs as shell and needs su.
		b, err = exec.CommandContext(ctx, "su", "0", "cat", m.Path).Output()
		if err != nil {
			return "", err
		}
	}
	return lastToken(b), nil
}

// lastToken returns the last usertoken in an MMKV file. MMKV is
// append-only between compactions, so an updated key's older values
// are still in the file ahead of the current one. The file starts
// with a little-endian uint32 of the valid length; bytes past it are
// leftovers from before the last compaction and can hold a stale token.
func lastToken(b []byte) string {
	if len(b) < 4 {
		return ""
	}
	n := binary.LittleEndian.Uint32(b)
	b = b[4:]
	if uint64(n) < uint64(len(b)) {
		b = b[:n]
	}
	all := tokenRe.FindAll(b, -1)
	if len(all) == 0 {
		return ""
	}
	return string(all[len(all)-1])
}

// TokenFile is the usertoken the remote logged in with, kept in a file
// so it outlives a server restart. Token hands out whichever of it and
// Fallback's (the app's own, via MMKVToken) was issued later, so
// logging in from the remote or from the app both take effect.
type TokenFile struct {
	Path     string
	Fallback TokenSource
}

func (f TokenFile) Save(token string) error {
	return os.WriteFile(f.Path, []byte(token), 0o600)
}

func (f TokenFile) Token(ctx context.Context) (string, error) {
	b, err := os.ReadFile(f.Path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	saved := strings.TrimSpace(string(b))
	app, err := f.Fallback.Token(ctx)
	if err != nil {
		if saved != "" {
			return saved, nil
		}
		return "", err
	}
	if issued(app) > issued(saved) {
		return app, nil
	}
	return saved, nil
}

// issued reads the unix time a usertoken carries in its third field
// (<hex32>::<hex32>::<unixtime>::ni), or 0 if it has none.
func issued(token string) int64 {
	parts := strings.Split(token, "::")
	if len(parts) < 3 {
		return 0
	}
	n, _ := strconv.ParseInt(parts[2], 10, 64)
	return n
}
