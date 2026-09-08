package wsserver

import (
	"context"
	"encoding/json"
	"fmt"
)

// DeviceController is the subset of adbctl.Session's behavior wsserver
// depends on. Satisfied by *adbctl.Session in production.
type DeviceController interface {
	KeyEvent(code string) error
	Text(s string) error
	LaunchApp(pkg string) error
	PointerMove(dx, dy int) error
	PointerButton(down bool) error
	Apps() []string
	RefreshApps(ctx context.Context) ([]string, error)
	Connected() bool
	// SetManualTarget overrides the target used for the next connection
	// attempt only (see adbctl.Session) — a one-shot hint, not a sticky
	// override. Automatic mDNS discovery resumes on the following drop.
	SetManualTarget(host string, port int)
}

type incoming struct {
	Type  string `json:"type"`
	Dx    int    `json:"dx"`
	Dy    int    `json:"dy"`
	Code  string `json:"code"`
	Value string `json:"value"`
	Pkg   string `json:"pkg"`
	Host  string `json:"host"`
	Port  int    `json:"port"`
}

// Dispatch parses one raw client WS message and invokes the matching
// method on ctrl. Returns an error for malformed JSON, an unknown
// message type, or a rejected command; callers should log and drop the
// message rather than closing the connection.
func Dispatch(ctx context.Context, ctrl DeviceController, raw []byte) error {
	var msg incoming
	if err := json.Unmarshal(raw, &msg); err != nil {
		return fmt.Errorf("wsserver: invalid json: %w", err)
	}
	switch msg.Type {
	case "move":
		return ctrl.PointerMove(msg.Dx, msg.Dy)
	case "pointerdown":
		return ctrl.PointerButton(true)
	case "pointerup":
		return ctrl.PointerButton(false)
	case "key":
		return ctrl.KeyEvent(msg.Code)
	case "text":
		return ctrl.Text(msg.Value)
	case "launch":
		return ctrl.LaunchApp(msg.Pkg)
	case "refresh_apps":
		_, err := ctrl.RefreshApps(ctx)
		return err
	case "set_target":
		if msg.Host == "" {
			return fmt.Errorf("wsserver: set_target: host must not be empty")
		}
		if msg.Port <= 0 || msg.Port > 65535 {
			return fmt.Errorf("wsserver: set_target: invalid port %d", msg.Port)
		}
		ctrl.SetManualTarget(msg.Host, msg.Port)
		return nil
	default:
		return fmt.Errorf("wsserver: unknown message type %q", msg.Type)
	}
}
