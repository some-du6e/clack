//go:build windows

package desktop

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// StartupFlag is the switch the Run key starts Clack with, as Slack's
// launch on login starts Slack with --startup (environment.invokedOnStartup,
// main.bundle.cjs 4.53.276 win32 @1137766): Launch Control's Keep in
// Background (hideOnStartup) then leaves the window in the notification
// area.
const StartupFlag = "--startup"

// runValue is the Run key's command for exe.
func runValue(exe string) string { return `"` + exe + `" ` + StartupFlag }

// LaunchOnLoginSupported reports whether this OS has Launch app on login.
func LaunchOnLoginSupported() bool { return true }

// LaunchOnLogin reports whether the per-user Run key starts Clack.
func LaunchOnLogin() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue("Clack")
	return err == nil
}

// SetLaunchOnLogin sets or deletes the Run key's Clack value.
func SetLaunchOnLogin(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue("Clack"); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	exe, err := autostartExe()
	if err != nil {
		return err
	}
	return k.SetStringValue("Clack", runValue(exe))
}

// RefreshLaunchOnLogin rewrites the Run value an older Clack wrote for
// this executable (the bare quoted path, which starts Clack without
// StartupFlag) as SetLaunchOnLogin writes it now. A value naming another
// copy, or already current, stays as it is.
func RefreshLaunchOnLogin() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return nil // no Run key: nothing to refresh
	}
	defer k.Close()
	v, _, err := k.GetStringValue("Clack")
	if err != nil {
		return nil
	}
	exe, err := autostartExe()
	if err != nil {
		return err
	}
	if v != `"`+exe+`"` {
		return nil
	}
	return k.SetStringValue("Clack", runValue(exe))
}
