package slack

import "sync/atomic"

// platformHook replaces the OS's answer in tests (SetPlatformMimeTypes).
var platformHook atomic.Pointer[func(ext string) (string, bool)]

// platformMimeType is Chromium's GetPlatformMimeTypeFromExtension, which
// FileType asks between its two lists: the type this machine's OS gives a
// file of extension ext (no "."), if it gives one. osMimeType is this OS's:
// the registry on Windows, UTType on macOS, the XDG database on Linux.
func platformMimeType(ext string) (string, bool) {
	if f := platformHook.Load(); f != nil {
		return (*f)(ext)
	}
	return osMimeType(ext)
}

// SetPlatformMimeTypes replaces the OS's lookup that FileType asks between
// Chromium's lists, and returns the undo: for tests, since every machine's
// OS gives its own types (a Linux runner's shared-mime-info types .go
// text/x-go). A nil f is an OS that gives none.
func SetPlatformMimeTypes(f func(ext string) (string, bool)) (restore func()) {
	if f == nil {
		f = func(string) (string, bool) { return "", false }
	}
	prev := platformHook.Swap(&f)
	return func() { platformHook.Store(prev) }
}
