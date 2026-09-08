package deviceio

import (
	"context"
	"fmt"
	"regexp"
	"sync"
)

var pkgNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.]*$`)

// keyCodes maps the Android KEYCODE_* names the PWA sends to the
// Linux evdev scancode Android's Generic.kl keylayout maps to that
// action. Sourced directly from
// /system/usr/keylayout/Generic.kl on the target device
// (t950s_be311) rather than assumed from Android keycode names —
// several entries differ from the "obvious" guess: HOME is scancode
// 172 (Generic.kl's HOME line), not KEY_HOME's 102 (which Generic.kl
// maps to MOVE_HOME, a text-editing action); DPAD_CENTER is 353
// (KEY_OK), not KEY_ENTER's 28.
//
// Verified live against the device: HOME and BACK confirmed via
// dumpsys window mCurrentFocus changing exactly as the physical key
// would; VOLUME_UP/DOWN confirmed via dumpsys audio stream volume
// actually changing. The rest share the identical injection path and
// the same authoritative keylayout source.
var keyCodes = map[string]uint16{
	"KEYCODE_BACK":             158,
	"KEYCODE_HOME":             172,
	"KEYCODE_DPAD_UP":          103,
	"KEYCODE_DPAD_DOWN":        108,
	"KEYCODE_DPAD_LEFT":        105,
	"KEYCODE_DPAD_RIGHT":       106,
	"KEYCODE_DPAD_CENTER":      353,
	"KEYCODE_VOLUME_UP":        115,
	"KEYCODE_VOLUME_DOWN":      114,
	"KEYCODE_VOLUME_MUTE":      113,
	"KEYCODE_POWER":            116,
	"KEYCODE_MEDIA_PLAY_PAUSE": 164,
}

// Controller executes remote-control commands directly against local
// device I/O: a uinput virtual keyboard for discrete keys, direct
// writes to the real pointer node for the trackpad, and local
// subprocess execution for text entry and app launch. It's the
// on-device replacement for the old adbctl package's Controller/
// Session, which drove the identical actions over a persistent remote
// `adb shell` pipe.
type Controller struct {
	keyMu    sync.Mutex
	keyboard eventWriter

	mouseMu sync.Mutex
	mouse   eventWriter

	appsMu sync.RWMutex
	apps   []string
}

// newController builds a Controller from already-open event writers.
// Production callers use New, which performs the actual device setup;
// tests use this directly with fakes.
func newController(keyboard, mouse eventWriter) *Controller {
	return &Controller{keyboard: keyboard, mouse: mouse}
}

func (c *Controller) KeyEvent(name string) error {
	code, ok := keyCodes[name]
	if !ok {
		return fmt.Errorf("deviceio: unknown key code %q", name)
	}
	c.keyMu.Lock()
	defer c.keyMu.Unlock()
	if err := c.keyboard.WriteEvent(evKey, code, 1); err != nil {
		return err
	}
	if err := c.keyboard.WriteEvent(evSyn, synReport, 0); err != nil {
		return err
	}
	if err := c.keyboard.WriteEvent(evKey, code, 0); err != nil {
		return err
	}
	return c.keyboard.WriteEvent(evSyn, synReport, 0)
}

func (c *Controller) PointerMove(dx, dy int) error {
	c.mouseMu.Lock()
	defer c.mouseMu.Unlock()
	if err := c.mouse.WriteEvent(evRel, relX, int32(dx)); err != nil {
		return err
	}
	if err := c.mouse.WriteEvent(evRel, relY, int32(dy)); err != nil {
		return err
	}
	return c.mouse.WriteEvent(evSyn, synReport, 0)
}

func (c *Controller) PointerButton(down bool) error {
	v := int32(0)
	if down {
		v = 1
	}
	c.mouseMu.Lock()
	defer c.mouseMu.Unlock()
	if err := c.mouse.WriteEvent(evKey, btnLeft, v); err != nil {
		return err
	}
	return c.mouse.WriteEvent(evSyn, synReport, 0)
}

func (c *Controller) Text(s string) error {
	return runText(s)
}

func (c *Controller) LaunchApp(pkg string) error {
	if !pkgNameRe.MatchString(pkg) {
		return fmt.Errorf("deviceio: invalid package name %q", pkg)
	}
	return runLaunch(pkg)
}

func (c *Controller) Apps() []string {
	c.appsMu.RLock()
	defer c.appsMu.RUnlock()
	out := make([]string, len(c.apps))
	copy(out, c.apps)
	return out
}

func (c *Controller) RefreshApps(ctx context.Context) ([]string, error) {
	pkgs, err := fetchInstalledPackages(ctx)
	if err != nil {
		return nil, err
	}
	c.appsMu.Lock()
	c.apps = pkgs
	c.appsMu.Unlock()
	return pkgs, nil
}

// Connected always reports true. Unlike the old network-ADB session,
// there's no remote link that can drop out from under an already-
// running process: if device I/O setup fails, New returns an error
// and the server never starts. Kept only so wsserver's existing
// status-broadcast message still has something to report.
func (c *Controller) Connected() bool { return true }
