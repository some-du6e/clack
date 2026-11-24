//go:build !darwin && !windows

package core

// MarkDownloaded marks a completed download on macOS (the quarantine
// xattr) and Windows (the Zone.Identifier stream) only.
func MarkDownloaded(string, string) {}
