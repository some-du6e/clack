//go:build windows

package spell

import (
	"runtime"
	"testing"
)

// Which engine Windows gives en-US: its own spell checker when the
// language is installed, else Chromium's hybrid falls back to Hunspell.
func TestWindowsChecker(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	platformThreadInit()
	if factory == nil {
		t.Fatal("no ISpellCheckerFactory")
	}
	ok := WindowsSupports("en-US")
	t.Logf("Windows spell checker supports en-US: %v", ok)
	if !ok {
		t.Skip("no en-US Windows checker on this runner (Hunspell covers it in TestPlatformBackend)")
	}
	e, _ := openWindows("en-US")
	defer e.Close()
	if !e.Check("hello") || e.Check("helo") {
		t.Errorf("Check: hello %v, helo %v", e.Check("hello"), e.Check("helo"))
	}
	t.Logf("Suggest(helo) = %q", e.Suggest("helo"))
	if len(e.Suggest("helo")) == 0 {
		t.Error("no suggestions")
	}
}
