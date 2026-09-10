package deviceio

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// textRunner is the minimal surface Controller.Text needs to type a
// string into the device's focused field. Backed by a local exec of
// `input text` in production (execTextRunner); tests use a fake.
type textRunner interface {
	RunText(s string) error
}

// execTextRunner types a string via Android's `input text` command.
// Run locally with each argument as a separate argv entry (no shell
// in the loop), so no shell-escaping is needed the way the old
// adb-shell-piped version required.
type execTextRunner struct{}

func (execTextRunner) RunText(s string) error {
	return exec.Command("input", "text", s).Run()
}

// runLaunch starts pkg's default launcher activity. Using
// ACTION_MAIN/CATEGORY_LAUNCHER with -p (package-restricted
// resolution) launches the right activity without needing to know its
// class name, unlike `am start -n`.
func runLaunch(pkg string) error {
	return exec.Command("am", "start",
		"-a", "android.intent.action.MAIN",
		"-c", "android.intent.category.LAUNCHER",
		"-p", pkg,
	).Run()
}

func fetchInstalledPackages(ctx context.Context) ([]string, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "pm", "list", "packages", "-3").Output()
	if err != nil {
		return nil, err
	}
	return parsePackageList(string(out)), nil
}

// runSetVolume sets STREAM_MUSIC's volume to an absolute level
// (stream 3). Uses `cmd media_session` directly rather than the
// `media` wrapper script: this device doesn't ship one
// (`/system/bin/media` doesn't exist here, confirmed against the live
// device), while `cmd media_session` is the lower-level service call
// that wrapper normally just forwards to on stock AOSP, and it's
// present and working here.
func runSetVolume(level int) error {
	return exec.Command("cmd", "media_session", "volume", "--stream", "3", "--set", strconv.Itoa(level)).Run()
}

// fetchVolume reads STREAM_MUSIC's current level from `dumpsys
// audio`, the same command confirmed working interactively against
// the real device (see parseStreamMusicVolume for the output shape).
func fetchVolume() (int, error) {
	cctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "dumpsys", "audio").Output()
	if err != nil {
		return 0, err
	}
	return parseStreamMusicVolume(string(out))
}

// parseStreamMusicVolume extracts the "streamVolume:" value from the
// "- STREAM_MUSIC:" block of `dumpsys audio` output. Other streams
// (STREAM_VOICE_CALL, STREAM_RING, ...) are aliased to STREAM_MUSIC
// in their own blocks and must not be matched instead.
func parseStreamMusicVolume(output string) (int, error) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	inBlock := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "- STREAM_MUSIC:":
			inBlock = true
		case strings.HasPrefix(line, "- STREAM_"):
			inBlock = false
		case inBlock:
			if v, ok := strings.CutPrefix(line, "streamVolume:"); ok {
				return strconv.Atoi(strings.TrimSpace(v))
			}
		}
	}
	return 0, fmt.Errorf("deviceio: streamVolume not found in dumpsys audio output")
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
