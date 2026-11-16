//go:build !windows && !linux

package ui

import "os/exec"

// configureChild does nothing here: no console to hide, no parent-death
// signal (the renderer stops at its own deadline).
func configureChild(*exec.Cmd) {}

// endStartupFeedback: no launch feedback to end off Windows.
func endStartupFeedback() {}
