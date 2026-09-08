package adbctl

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Session owns a persistent `adb shell` subprocess connected to a single
// target device (e.g. "10.102.78.155:3333"), keeping it alive and
// reconnecting on failure. It's the type wsserver depends on to drive
// the device.
type Session struct {
	target   string
	onStatus func(connected bool)

	mu   sync.RWMutex
	ctrl *Controller
	cmd  *exec.Cmd

	statusMu  sync.RWMutex
	connected bool

	appsMu sync.RWMutex
	apps   []string
}

func NewSession(target string, onStatus func(connected bool)) *Session {
	return &Session{target: target, onStatus: onStatus}
}

// Run connects and keeps reconnecting with backoff until ctx is canceled.
func (s *Session) Run(ctx context.Context) {
	attempt := 0
	for ctx.Err() == nil {
		if err := s.connectOnce(ctx); err != nil {
			log.Printf("adbctl: connect to %s failed: %v", s.target, err)
			s.setStatus(false)
			select {
			case <-ctx.Done():
				return
			case <-time.After(nextBackoff(attempt)):
			}
			attempt++
			continue
		}

		attempt = 0
		s.setStatus(true)

		s.mu.RLock()
		cmd := s.cmd
		s.mu.RUnlock()
		_ = cmd.Wait() // blocks until the shell exits (device dropped, killed, etc.)

		s.setStatus(false)
	}
}

func nextBackoff(attempt int) time.Duration {
	steps := []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second}
	if attempt < 0 {
		attempt = 0
	}
	if attempt >= len(steps) {
		attempt = len(steps) - 1
	}
	return steps[attempt]
}

func (s *Session) connectOnce(ctx context.Context) error {
	if out, err := exec.CommandContext(ctx, "adb", "connect", s.target).CombinedOutput(); err != nil {
		return fmt.Errorf("adb connect: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	cmd := exec.CommandContext(ctx, "adb", "-s", s.target, "shell")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start shell: %w", err)
	}

	ctrl := New(&lineWriter{w: stdin})

	nodePath, err := s.resolveMouseNode(ctx)
	if err != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("resolve mouse node: %w", err)
	}
	ctrl.SetMouseNode(nodePath)

	s.mu.Lock()
	s.cmd = cmd
	s.ctrl = ctrl
	s.mu.Unlock()

	if pkgs, err := s.fetchInstalledPackages(ctx); err == nil {
		s.appsMu.Lock()
		s.apps = pkgs
		s.appsMu.Unlock()
	} else {
		log.Printf("adbctl: list packages failed: %v", err)
	}

	return nil
}

func (s *Session) resolveMouseNode(ctx context.Context) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(cctx, "adb", "-s", s.target, "shell", "getevent", "-i")
	out, _ := cmd.CombinedOutput() // getevent -i never exits on its own; the timeout always kills it
	return parseMouseNodeFromGetevent(string(out), "jixin_mouse")
}

func parseMouseNodeFromGetevent(output, wantName string) (string, error) {
	var currentPath string
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "add device"):
			if idx := strings.Index(line, "/dev/input/"); idx >= 0 {
				currentPath = line[idx:]
			}
		case strings.HasPrefix(line, "name:"):
			name := strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "name:")), `"`)
			if name == wantName && currentPath != "" {
				return currentPath, nil
			}
		}
	}
	return "", fmt.Errorf("adbctl: no input device named %q found", wantName)
}

func (s *Session) fetchInstalledPackages(ctx context.Context) ([]string, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "adb", "-s", s.target, "shell", "pm", "list", "packages", "-3").Output()
	if err != nil {
		return nil, err
	}
	return parsePackageList(string(out)), nil
}

func parsePackageList(output string) []string {
	var pkgs []string
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if pkg, ok := strings.CutPrefix(line, "package:"); ok && pkg != "" {
			pkgs = append(pkgs, pkg)
		}
	}
	return pkgs
}

func (s *Session) setStatus(connected bool) {
	s.statusMu.Lock()
	s.connected = connected
	s.statusMu.Unlock()
	if s.onStatus != nil {
		s.onStatus(connected)
	}
}

// Connected reports the last known device-connection state.
func (s *Session) Connected() bool {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()
	return s.connected
}

// Apps returns the most recently fetched list of installed third-party
// package names.
func (s *Session) Apps() []string {
	s.appsMu.RLock()
	defer s.appsMu.RUnlock()
	out := make([]string, len(s.apps))
	copy(out, s.apps)
	return out
}

// RefreshApps re-fetches the installed package list from the device.
func (s *Session) RefreshApps(ctx context.Context) ([]string, error) {
	pkgs, err := s.fetchInstalledPackages(ctx)
	if err != nil {
		return nil, err
	}
	s.appsMu.Lock()
	s.apps = pkgs
	s.appsMu.Unlock()
	return pkgs, nil
}

func (s *Session) currentController() (*Controller, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ctrl == nil {
		return nil, fmt.Errorf("adbctl: not connected to device yet")
	}
	return s.ctrl, nil
}

func (s *Session) KeyEvent(code string) error {
	c, err := s.currentController()
	if err != nil {
		return err
	}
	return c.KeyEvent(code)
}

func (s *Session) Text(text string) error {
	c, err := s.currentController()
	if err != nil {
		return err
	}
	return c.Text(text)
}

func (s *Session) LaunchApp(pkg string) error {
	c, err := s.currentController()
	if err != nil {
		return err
	}
	return c.LaunchApp(pkg)
}

func (s *Session) PointerMove(dx, dy int) error {
	c, err := s.currentController()
	if err != nil {
		return err
	}
	return c.PointerMove(dx, dy)
}

func (s *Session) PointerButton(down bool) error {
	c, err := s.currentController()
	if err != nil {
		return err
	}
	return c.PointerButton(down)
}
