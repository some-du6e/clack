//go:build !linux && !windows && !(darwin && cgo)

package ui

// No OS reduce-motion setting here: Slack's Allow animations decides.
func startOSMotionWatcher() {}
