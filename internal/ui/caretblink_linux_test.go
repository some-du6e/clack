//go:build linux

package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The caret's blink on Linux: half GTK's gtk-cursor-blink-time, none when
// gtk-cursor-blink is off, GTK's 1200 ms when nothing sets it (Chromium's
// OsSettingsProviderGtk::CaretBlinkInterval); settings.ini when the
// desktop's settings portal has nothing, the user's before the system's.
func TestGtkCaretBlink(t *testing.T) {
	home, sys := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("XDG_CONFIG_DIRS", sys)
	write := func(dir, v, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, v), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, v, "settings.ini"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := gtkCaretBlink(nil); got != 600*time.Millisecond {
		t.Errorf("nothing set: %v, want GTK's 1200 ms halved", got)
	}
	write(sys, "gtk-3.0", "[Settings]\ngtk-cursor-blink-time=1000\n")
	if got := gtkCaretBlink(nil); got != 500*time.Millisecond {
		t.Errorf("the system's 1000 ms: %v, want 500 ms", got)
	}
	write(home, "gtk-3.0", "[Other]\ngtk-cursor-blink-time=200\n[Settings]\ngtk-theme-name=Adwaita\ngtk-cursor-blink-time = 1600\n")
	if got := gtkCaretBlink(nil); got != 800*time.Millisecond {
		t.Errorf("the user's 1600 ms: %v, want 800 ms (the [Settings] group's, before the system's)", got)
	}
	write(home, "gtk-3.0", "[Settings]\ngtk-cursor-blink=false\n")
	if got := gtkCaretBlink(nil); got != 0 {
		t.Errorf("blinking off: %v, want 0 (no blinking)", got)
	}
}
