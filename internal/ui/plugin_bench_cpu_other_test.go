//go:build !unix && !windows

package ui

import "time"

// procCPU isn't measured here (the bench runs on Linux).
func procCPU() time.Duration { return 0 }
