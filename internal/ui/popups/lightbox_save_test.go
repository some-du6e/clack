package popups

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLightboxSaveSanitizesName(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Downloads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"../../.bashrc", "/etc/passwd", `..\..\evil.exe`, "C:\\Windows\\win.ini", "a/../../b.png",
		"..", ".", "   ", "\x00\x01", "CON.png", "nul", "com1.txt", strings.Repeat("é", 150) + ".png",
	} {
		p, err := lbSaveIn(dir, name, "https://files.slack.com/files-pri/T-F/x.png", []byte("\x89PNG\r\n\x1a\n"))
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		if filepath.Dir(p) != dir {
			t.Errorf("%q saved outside Downloads: %s", name, p)
		}
		base := filepath.Base(p)
		if strings.HasPrefix(base, ".") || strings.ContainsAny(base, `/\:`) {
			t.Errorf("%q saved as %q", name, base)
		}
		if up := strings.ToUpper(base); strings.HasPrefix(up, "CON.") || up == "NUL" || strings.HasPrefix(up, "COM1.") {
			t.Errorf("%q saved as a Windows device name %q", name, base)
		}
	}
	if entries, _ := os.ReadDir(root); len(entries) != 1 {
		t.Errorf("files written next to Downloads: %v", entries)
	}

	// An existing file, or a symlink planted under the name, is never
	// overwritten or followed.
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(target, filepath.Join(dir, "link.png")); err != nil {
			t.Fatal(err)
		}
		p, err := lbSaveIn(dir, "link.png", "", []byte("new"))
		if err != nil || filepath.Base(p) != "link (1).png" {
			t.Errorf("symlink name: %q %v", p, err)
		}
		if b, _ := os.ReadFile(target); string(b) != "keep" {
			t.Error("symlink target overwritten")
		}
	}
}
