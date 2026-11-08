//go:build !linux && !windows && !(darwin && cgo)

package ui

// No OS colour scheme here: "System" stays light.
func osPrefersDark() (dark, ok bool) { return false, false }

func startOSSchemeWatcher() {}
