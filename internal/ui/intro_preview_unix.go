//go:build !windows && !linux

package ui

import "os/exec"

// configureChild does nothing here: no console to hide, no parent-death
// signal (a renderer whose window died fails its write, EPIPE, cleans up
// and exits: handleChildSignals).
func configureChild(*exec.Cmd) {}

// endStartupFeedback: no launch feedback to end off Windows.
func endStartupFeedback() {}
