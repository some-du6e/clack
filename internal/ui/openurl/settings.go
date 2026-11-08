package openurl

import (
	"log"
	"os/exec"
	"runtime"
	"strings"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/uiguard"
)

// settingsPrefixes are the system-settings links OpenSettings passes on:
// macOS's Privacy panes and Windows's privacy pages (avperm.SettingsURL).
var settingsPrefixes = map[string][]string{
	"darwin":  {"x-apple.systempreferences:com.apple.preference.security?Privacy_"},
	"windows": {"ms-settings:privacy-"},
}

// CheckSettings returns u if it is a privacy-settings link for this OS.
func CheckSettings(u string) (string, error) {
	if len(u) > 200 || strings.ContainsAny(u, " \t\r\n\\\"`<>&|;$") {
		return "", ErrRefused
	}
	for _, p := range settingsPrefixes[runtime.GOOS] {
		if rest, ok := strings.CutPrefix(u, p); ok && rest != "" {
			for _, r := range rest {
				if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-' || r == '_') {
					return "", ErrRefused
				}
			}
			return u, nil
		}
	}
	return "", ErrRefused
}

// OpenSettings opens a system settings page (the camera or microphone
// privacy pane) without blocking: the exec runs on its own goroutine.
func OpenSettings(raw string) error {
	u, err := CheckSettings(raw)
	if err != nil {
		return err
	}
	go func() {
		uiguard.OffUI("exec (open system settings)")
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
		} else {
			cmd = exec.Command("open", u)
		}
		cmd.WaitDelay = waitDelay
		if err := cmd.Start(); err != nil {
			log.Printf("openurl: %v", err)
			return
		}
		_ = cmd.Wait()
	}()
	return nil
}
