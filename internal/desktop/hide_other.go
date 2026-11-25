//go:build !windows

package desktop

// hidePath: outside Windows the stage's leading dot hides it.
func hidePath(string) {}
