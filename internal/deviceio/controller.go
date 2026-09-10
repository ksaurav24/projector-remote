package deviceio

import (
	"context"
	"fmt"
	"regexp"
	"strings"
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
	"KEYCODE_DEL":              14,
	"KEYCODE_ENTER":            28,
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

	// textMu guards lastSyncedText, which tracks what Text has most
	// recently typed into the device's focused field so it can send
	// only the incremental change on the next call (see Text).
	textMu         sync.Mutex
	lastSyncedText string
	textRun        textRunner
}

// newController builds a Controller from already-open event writers.
// Production callers use New, which performs the actual device setup;
// tests use this directly with fakes (and may override c.textRun with
// their own fake textRunner for Text-specific assertions).
func newController(keyboard, mouse eventWriter) *Controller {
	return &Controller{keyboard: keyboard, mouse: mouse, textRun: execTextRunner{}}
}

func (c *Controller) KeyEvent(name string) error {
	code, ok := keyCodes[name]
	if !ok {
		return fmt.Errorf("deviceio: unknown key code %q", name)
	}
	return c.pressKey(code)
}

// pressKey injects one press+release of a raw evdev key code. Shared
// by KeyEvent (named codes from the PWA) and Text (synthetic DEL
// presses used to correct the remote field, see below).
func (c *Controller) pressKey(code uint16) error {
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

// Text keeps the device's focused text field in sync with value.
// Android's `input text` command *appends* keystrokes into whatever
// field has focus - it has no "replace" primitive - so calling this
// repeatedly with a naively-resent full string would duplicate text
// on every call. Instead this tracks the last value it successfully
// typed and sends only the delta:
//   - if value extends what's already there, type just the new suffix.
//   - otherwise (an edit or deletion happened), press DEL enough
//     times to clear the old text, then type value fresh.
//
// Designed for a debounced "live" text field on the client: call it
// with the field's current full value after each pause in typing.
func (c *Controller) Text(value string) error {
	c.textMu.Lock()
	defer c.textMu.Unlock()

	if value == c.lastSyncedText {
		return nil
	}

	if strings.HasPrefix(value, c.lastSyncedText) {
		suffix := value[len(c.lastSyncedText):]
		if suffix != "" {
			if err := c.textRun.RunText(suffix); err != nil {
				return err
			}
		}
	} else {
		delCode := keyCodes["KEYCODE_DEL"]
		for range c.lastSyncedText {
			if err := c.pressKey(delCode); err != nil {
				return err
			}
		}
		if value != "" {
			if err := c.textRun.RunText(value); err != nil {
				return err
			}
		}
	}

	c.lastSyncedText = value
	return nil
}

func (c *Controller) LaunchApp(pkg string) error {
	if !pkgNameRe.MatchString(pkg) {
		return fmt.Errorf("deviceio: invalid package name %q", pkg)
	}
	return runLaunch(pkg)
}

// Volume returns the device's current STREAM_MUSIC volume, 0-100.
func (c *Controller) Volume() (int, error) {
	return fetchVolume()
}

// SetVolume sets the device's STREAM_MUSIC volume to an absolute
// level, clamped to 0-100.
func (c *Controller) SetVolume(level int) error {
	return runSetVolume(clampVolume(level))
}

func clampVolume(level int) int {
	if level < 0 {
		return 0
	}
	if level > 100 {
		return 100
	}
	return level
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
