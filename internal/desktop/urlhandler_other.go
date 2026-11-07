//go:build !windows

package desktop

import (
	"errors"
	"os/exec"
	"runtime"
)

// RegisterURLHandler makes Clack the user's slack:// handler. On Linux it
// asks xdg-mime to use clack.desktop (installed by the .deb/.rpm or an
// AppImage integration tool). macOS registers through the app bundle's
// Info.plist when Clack.app is first opened.
func RegisterURLHandler() error {
	if runtime.GOOS == "darwin" {
		return errors.New("on macOS, open Clack.app once; its Info.plist registers slack://")
	}
	return exec.Command("xdg-mime", "default", "clack.desktop", "x-scheme-handler/slack").Run()
}

// UnregisterURLHandler is only needed on Windows; elsewhere the package
// manager or another app's registration replaces Clack's.
func UnregisterURLHandler() error {
	return errors.New("only needed on Windows: uninstall the package or pick another default slack:// app")
}
