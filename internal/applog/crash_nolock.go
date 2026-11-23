//go:build !windows && !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package applog

import "os"

// openCrashFile opens crash.log for this run (no lock on this OS).
func openCrashFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
}
