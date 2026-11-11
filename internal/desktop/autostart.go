package desktop

import (
	"os"
	"path/filepath"
	"strings"
)

// Launch on login: Slack desktop's DesktopPref "Launch app on login"
// (settings.launchOnStartup, Windows and Linux). On Linux it's an XDG
// autostart entry, on Windows the per-user Run key; macOS has neither
// row (Slack's is the Dock's Options > Open at Login).

// autostartExe is what the entry starts: the AppImage when running from
// one (the mounted binary goes away), else this executable.
func autostartExe() (string, error) {
	if p := os.Getenv("APPIMAGE"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return p, nil
}

// desktopExecQuote quotes one Exec argument as the Desktop Entry spec
// says: in double quotes, with ", `, $ and \ escaped.
func desktopExecQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`)
	return `"` + r.Replace(s) + `"`
}

// autostartEntry is the Linux autostart file's text for exe.
func autostartEntry(exe string) string {
	return "[Desktop Entry]\nType=Application\nName=Clack\nComment=Launch Clack on login\nExec=" + desktopExecQuote(exe) +
		"\nTerminal=false\nX-GNOME-Autostart-enabled=true\n"
}

// autostartDir is $XDG_CONFIG_HOME/autostart (~/.config/autostart).
func autostartDir() (string, error) {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" && filepath.IsAbs(d) {
		return filepath.Join(d, "autostart"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "autostart"), nil
}

// autostartFile is Clack's autostart entry.
const autostartFile = "clack.desktop"
