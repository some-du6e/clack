//go:build !linux

package ui

// startCaretBlinkWatcher: Windows reads GetCaretBlinkTime and macOS its
// insertion point's blink period as each window opens (Gio patches 13 and
// 47).
func startCaretBlinkWatcher() {}
