//go:build !linux && !darwin

package ui

import "time"

// threadCPU is -1 here: Windows' thread times move in 15.6 ms ticks, too
// coarse for a frame, so the frame tests take the clock's time.
func threadCPU() time.Duration { return -1 }
