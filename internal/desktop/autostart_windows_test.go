//go:build windows

package desktop

import (
	"testing"

	"golang.org/x/sys/windows/registry"
)

// Launch on login's Run value (CLACK_WINDOWS_TRAY_TESTS: it writes the
// runner user's HKCU Run key, and puts back what was there): Clack starts
// with --startup (Slack's invokedOnStartup), an older Clack's bare value
// for this exe gets the flag, another copy's value is left alone, and off
// removes it.
func TestLaunchOnLoginRunValue(t *testing.T) {
	trayShellTests(t)
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	if old, _, err := k.GetStringValue("Clack"); err == nil {
		t.Cleanup(func() { _ = k.SetStringValue("Clack", old) })
	} else {
		t.Cleanup(func() { _ = k.DeleteValue("Clack") })
	}
	exe, err := autostartExe()
	if err != nil {
		t.Fatal(err)
	}
	value := func() string {
		v, _, err := k.GetStringValue("Clack")
		if err != nil {
			return "<none>"
		}
		return v
	}
	want := `"` + exe + `" --startup`
	if err := SetLaunchOnLogin(true); err != nil {
		t.Fatal(err)
	}
	if got := value(); got != want || !LaunchOnLogin() {
		t.Fatalf("on: %q (%v), want %q", got, LaunchOnLogin(), want)
	}
	if err := k.SetStringValue("Clack", `"`+exe+`"`); err != nil { // an older Clack's
		t.Fatal(err)
	}
	if err := RefreshLaunchOnLogin(); err != nil {
		t.Fatal(err)
	}
	if got := value(); got != want {
		t.Errorf("refreshed: %q, want %q", got, want)
	}
	other := `"C:\Other\Clack.exe"`
	if err := k.SetStringValue("Clack", other); err != nil {
		t.Fatal(err)
	}
	if err := RefreshLaunchOnLogin(); err != nil {
		t.Fatal(err)
	}
	if got := value(); got != other {
		t.Errorf("another copy's value became %q", got)
	}
	if err := SetLaunchOnLogin(false); err != nil {
		t.Fatal(err)
	}
	if got := value(); got != "<none>" || LaunchOnLogin() {
		t.Errorf("off: %q (%v)", got, LaunchOnLogin())
	}
}
