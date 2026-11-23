//go:build !darwin

package core

// quarantine marks a completed download on macOS only
// (download_quarantine_darwin.go).
func quarantine(string) {}
