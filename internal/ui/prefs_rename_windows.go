//go:build windows

package ui

import (
	"errors"
	"os"
	"syscall"
)

// renameRefused reports whether a failed rename of ui.json.tmp over p is
// Windows refusing it while another handle has a file open without
// FILE_SHARE_DELETE: ERROR_ACCESS_DENIED for p, ERROR_SHARING_VIOLATION
// for the tmp file. Those clear when the handle closes, so writePrefsFile
// tries again. A read-only p, or a directory there, is refused with
// ERROR_ACCESS_DENIED for good, and fails at once, as every other error
// does.
func renameRefused(p string, err error) bool {
	const errorAccessDenied, errorSharingViolation = syscall.Errno(5), syscall.Errno(32)
	switch {
	case errors.Is(err, errorSharingViolation):
		return true
	case errors.Is(err, errorAccessDenied):
		fi, e := os.Stat(p)
		return e == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o200 != 0
	}
	return false
}
