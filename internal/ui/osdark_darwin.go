//go:build darwin

package ui

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// osPrefersDark reads macOS' appearance (AppleInterfaceStyle is "Dark" in
// dark mode and absent in light mode).
func osPrefersDark() (dark, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "defaults", "read", "-g", "AppleInterfaceStyle").Output()
	if err != nil {
		if _, isExit := err.(*exec.ExitError); isExit {
			return false, true // the key is unset in light mode
		}
		return false, false
	}
	return strings.TrimSpace(string(out)) == "Dark", true
}
