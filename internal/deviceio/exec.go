package deviceio

import (
	"bufio"
	"context"
	"os/exec"
	"strings"
	"time"
)

// runText types a string via Android's `input text` command. Run
// locally with each argument as a separate argv entry (no shell in
// the loop), so no shell-escaping is needed the way the old
// adb-shell-piped version required.
func runText(s string) error {
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
