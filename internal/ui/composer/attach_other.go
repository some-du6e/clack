//go:build (!linux && !windows && !darwin) || android || ios

package composer

import "context"

// No clipboard images or copied files; the picker is the explorer or the
// in-app modal. Windows is attach_windows.go, macOS attach_darwin.go.

// attExplorerUsable reports whether the Gio explorer's chooser is safe to
// call; the portal-hang problem is Linux only.
func attExplorerUsable() bool { return true }

func attClipboardSupported() bool { return false }

func attClipboardFiles(context.Context) ([]string, error) { return nil, nil }

func attClipboardImage(context.Context, bool) ([]byte, string, error) {
	return nil, "", errAttClipNone
}

func attNativePick(context.Context) ([]string, bool, error) { return nil, false, nil }
