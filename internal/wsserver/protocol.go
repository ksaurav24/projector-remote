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
}

type incoming struct {
	Type  string `json:"type"`
	Dx    int    `json:"dx"`
	Dy    int    `json:"dy"`
	Code  string `json:"code"`
	Value string `json:"value"`
	Pkg   string `json:"pkg"`
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
	default:
		return fmt.Errorf("wsserver: unknown message type %q", msg.Type)
	}
}
