//go:build !windows

package applog

// CrashRecordFor is Windows' record of the previous run's end; nothing
// keeps one here.
func CrashRecordFor(prev string) string { return "" }
