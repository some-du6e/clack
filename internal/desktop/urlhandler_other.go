//go:build !windows

package desktop

import (
	"errors"
	"os/exec"
	"runtime"
	"time"
)

// URLHandlerAtStart is Windows only. Linux lists Clack as a slack://
// handler through clack.desktop's MimeType (installed by the .deb/.rpm or
// an AppImage integration tool) and never makes it the default by
// itself; macOS registers Clack.app's Info.plist when it is first opened.
func URLHandlerAtStart() bool { return false }

// EnsureURLHandler is Windows' registration at start; elsewhere it does
// nothing.
func EnsureURLHandler() (URLHandlerResult, error) { return URLHandlerResult{}, nil }

// RegisterURLHandler makes Clack the user's slack:// handler, on request
// (`clack --register-url-handler`). On Linux it asks xdg-mime to use
// clack.desktop. macOS registers through the app bundle's Info.plist when
// Clack.app is first opened.
func RegisterURLHandler() (URLHandlerResult, error) {
	if runtime.GOOS == "darwin" {
		return URLHandlerResult{}, errors.New("on macOS, open Clack.app once; its Info.plist registers slack://")
	}
	cmd := exec.Command("xdg-mime", "default", "clack.desktop", "x-scheme-handler/slack")
	cmd.WaitDelay = 5 * time.Second // never wait on a child's inherited pipes
	if err := cmd.Run(); err != nil {
		return URLHandlerResult{}, err
	}
	return URLHandlerResult{Changed: true, Default: true}, nil
}

// UnregisterURLHandler is only needed on Windows; elsewhere the package
// manager or another app's registration replaces Clack's.
func UnregisterURLHandler() (URLHandlerResult, error) {
	return URLHandlerResult{}, errors.New("only needed on Windows: uninstall the package or pick another default slack:// app")
}
