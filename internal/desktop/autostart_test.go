package desktop

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAutostartEntry(t *testing.T) {
	got := autostartEntry(`/home/k/My "Apps"/Clack $1.AppImage`)
	if !strings.Contains(got, "Exec=\"/home/k/My \\\"Apps\\\"/Clack \\$1.AppImage\"\n") || !strings.HasPrefix(got, "[Desktop Entry]\nType=Application\n") {
		t.Errorf("entry:\n%s", got)
	}
}

// Launch on login on Linux: the autostart entry under $XDG_CONFIG_HOME,
// written and removed (twice is fine).
func TestLaunchOnLoginLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux's autostart entry")
	}
	d := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", d)
	t.Setenv("APPIMAGE", "")
	if LaunchOnLogin() {
		t.Fatal("on before writing")
	}
	if err := SetLaunchOnLogin(true); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(d, "autostart", "clack.desktop"))
	if err != nil || !LaunchOnLogin() {
		t.Fatalf("after on: %v, %v", err, LaunchOnLogin())
	}
	exe, _ := autostartExe()
	if !strings.Contains(string(b), "Exec="+desktopExecQuote(exe)+"\n") {
		t.Errorf("entry doesn't start %s:\n%s", exe, b)
	}
	for i := 0; i < 2; i++ {
		if err := SetLaunchOnLogin(false); err != nil || LaunchOnLogin() {
			t.Fatalf("off (%d): %v, %v", i, err, LaunchOnLogin())
		}
	}
}
