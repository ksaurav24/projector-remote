package deviceio

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// resolveMouseNodePath runs `getevent -i` locally and returns the
// /dev/input/eventN path of the device named wantName (e.g.
// "jixin_mouse"). Device numbering isn't guaranteed stable across
// reboots, so this is resolved by name at startup rather than
// hardcoded.
func resolveMouseNodePath(ctx context.Context, wantName string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(cctx, "getevent", "-i")
	out, _ := cmd.CombinedOutput() // getevent -i never exits on its own; the timeout always kills it
	return parseMouseNodeFromGetevent(string(out), wantName)
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
	return "", fmt.Errorf("deviceio: no input device named %q found", wantName)
}

func openMouseNode(path string) (*fileEventWriter, error) {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("deviceio: open mouse node %s: %w", path, err)
	}
	return &fileEventWriter{f: f}, nil
}
