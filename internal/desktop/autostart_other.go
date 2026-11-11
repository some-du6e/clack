//go:build !linux && !windows

package desktop

import "errors"

// LaunchOnLoginSupported reports whether this OS has Launch app on login.
func LaunchOnLoginSupported() bool { return false }

// LaunchOnLogin is false where Clack has no login item.
func LaunchOnLogin() bool { return false }

// SetLaunchOnLogin is Linux and Windows only.
func SetLaunchOnLogin(bool) error { return errors.New("desktop: no launch on login on this platform") }
