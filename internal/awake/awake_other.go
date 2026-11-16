//go:build !windows

package awake

import "time"

// Go's monotonic clock already stands still while the computer sleeps
// here (see the package comment).
func now() int64 { return int64(time.Since(base)) + 1 }
