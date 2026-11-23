//go:build !windows

package applog

import "os"

// armNative is Windows' native fault capture (crash_windows.go). Elsewhere
// the runtime's own crash output already has native faults: a signal in C
// code (cgo, the macOS main thread's AppKit) is a Go fatal error there.
func armNative(*os.File, func()) bool { return false }

func releaseNative() {}
