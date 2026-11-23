//go:build !windows

package composer

// startPasteSnap: nil, the paste reads each part the platform's way.
func startPasteSnap(plain bool) *pasteSnap { return nil }
