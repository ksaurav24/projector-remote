package deviceio

import (
	"context"
	"fmt"
	"time"
)

const (
	uinputPath    = "/dev/uinput"
	mouseNodeName = "jixin_mouse"
)

// New opens the on-device inputs this package drives — a self-created
// uinput virtual keyboard (see openUinputKeyboard) and the real
// relative-pointer node (resolved by name, see resolveMouseNodePath)
// — and does an initial installed-package fetch.
//
// Returns an error if keyboard or mouse setup fails; callers should
// treat that as fatal. There's no reconnect loop the way the old
// network-ADB Session had one: this is local hardware that's either
// present at process start or isn't, not a link that can drop out
// from under an already-running process.
func New(ctx context.Context) (*Controller, error) {
	keyboard, err := openUinputKeyboard(uinputPath, keyCodes)
	if err != nil {
		return nil, err
	}
	// Give the kernel/InputReader a moment to pick up the freshly
	// created uinput device before the first injected event -
	// observed necessary during development (immediate injection
	// after UI_DEV_CREATE was unreliable).
	time.Sleep(500 * time.Millisecond)

	mouseNode, err := resolveMouseNodePath(ctx, mouseNodeName)
	if err != nil {
		return nil, fmt.Errorf("deviceio: %w", err)
	}
	mouse, err := openMouseNode(mouseNode)
	if err != nil {
		return nil, err
	}

	c := newController(keyboard, mouse)

	if pkgs, err := fetchInstalledPackages(ctx); err == nil {
		c.apps = pkgs
	}
	// A failed initial fetch is non-fatal: apps starts empty and
	// RefreshApps can retry later.

	return c, nil
}
