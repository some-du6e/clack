//go:build linux

package desktop

import (
	"errors"
	"os"
	"path/filepath"
)

// LaunchOnLoginSupported reports whether this OS has Launch app on login.
func LaunchOnLoginSupported() bool { return true }

// LaunchOnLogin reports whether Clack's autostart entry is there.
func LaunchOnLogin() bool {
	d, err := autostartDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(d, autostartFile))
	return err == nil
}

// SetLaunchOnLogin writes or removes the autostart entry.
func SetLaunchOnLogin(on bool) error {
	d, err := autostartDir()
	if err != nil {
		return err
	}
	p := filepath.Join(d, autostartFile)
	if !on {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	exe, err := autostartExe()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(autostartEntry(exe)), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// StartupFlag is Windows' launch-on-login switch (autostart_windows.go);
// this OS's autostart entry doesn't pass it.
const StartupFlag = "--startup"

// RefreshLaunchOnLogin is Windows' Run value upgrade: nothing to do here.
func RefreshLaunchOnLogin() error { return nil }
