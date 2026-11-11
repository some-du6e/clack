//go:build windows

package desktop

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

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
	return k.SetStringValue("Clack", `"`+exe+`"`)
}
