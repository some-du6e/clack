//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package applog

import (
	"errors"
	"os"
	"syscall"
)

// openCrashFile opens crash.log for this run under an exclusive flock: a
// second Clack (a launch that hands its link to the running one) can't
// start it afresh while the running one's crash output points at it. The
// lock lasts as long as the process (the runtime's duplicate of the
// descriptor holds it).
func openCrashFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, errCrashFileHeld
		}
		// a file system without locks (some network homes): the capture
		// runs unlocked rather than not at all
		return f, nil
	}
	return f, nil
}
