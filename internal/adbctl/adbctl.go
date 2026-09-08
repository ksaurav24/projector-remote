package adbctl

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
)

var (
	keyCodeRe = regexp.MustCompile(`^KEYCODE_[A-Z0-9_]+$`)
	pkgNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.]*$`)
)

// Writer is the minimal surface Controller needs to send one shell
// command line to the device. Production code backs it with the stdin
// of a persistent `adb shell` subprocess (see lineWriter); tests use an
// in-memory fake.
type Writer interface {
	WriteLine(s string) error
}

// lineWriter adapts an io.Writer (e.g. a subprocess's stdin pipe) to
// the Writer interface by appending a trailing newline per command.
type lineWriter struct {
	w io.Writer
}

func (l *lineWriter) WriteLine(s string) error {
	_, err := fmt.Fprintln(l.w, s)
	return err
}

// Controller formats and sends device-control commands over a shell
// Writer. It knows nothing about how that shell is connected — Session
// (session.go) owns the subprocess lifecycle.
type Controller struct {
	mu        sync.Mutex
	w         Writer
	mouseNode string
}

func New(w Writer) *Controller {
	return &Controller{w: w}
}

// SetMouseNode records the resolved /dev/input/eventN path for the
// device's relative-pointer input node, used by PointerMove/PointerButton.
func (c *Controller) SetMouseNode(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mouseNode = path
}

func (c *Controller) KeyEvent(code string) error {
	if !keyCodeRe.MatchString(code) {
		return fmt.Errorf("adbctl: invalid key code %q", code)
	}
	return c.writeLines(fmt.Sprintf("input keyevent %s", code))
}

func (c *Controller) Text(s string) error {
	return c.writeLines(fmt.Sprintf("input text %s", shellQuote(s)))
}

func (c *Controller) LaunchApp(pkg string) error {
	if !pkgNameRe.MatchString(pkg) {
		return fmt.Errorf("adbctl: invalid package name %q", pkg)
	}
	return c.writeLines(fmt.Sprintf("monkey -p %s -c android.intent.category.LAUNCHER 1", pkg))
}

func (c *Controller) PointerMove(dx, dy int) error {
	c.mu.Lock()
	node := c.mouseNode
	c.mu.Unlock()
	if node == "" {
		return fmt.Errorf("adbctl: mouse input node not resolved yet")
	}
	return c.writeLines(
		fmt.Sprintf("sendevent %s 2 0 %d", node, dx),
		fmt.Sprintf("sendevent %s 2 1 %d", node, dy),
		fmt.Sprintf("sendevent %s 0 0 0", node),
	)
}

func (c *Controller) PointerButton(down bool) error {
	c.mu.Lock()
	node := c.mouseNode
	c.mu.Unlock()
	if node == "" {
		return fmt.Errorf("adbctl: mouse input node not resolved yet")
	}
	v := 0
	if down {
		v = 1
	}
	return c.writeLines(
		fmt.Sprintf("sendevent %s 1 0x110 %d", node, v),
		fmt.Sprintf("sendevent %s 0 0 0", node),
	)
}

// writeLines sends all lines under a single lock so a compound gesture
// (e.g. move's X/Y/SYN triplet) can never be interleaved with another
// goroutine's command on the shared shell.
func (c *Controller) writeLines(lines ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, line := range lines {
		if err := c.w.WriteLine(line); err != nil {
			return err
		}
	}
	return nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
