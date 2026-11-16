//go:build !windows

package ui

import "os/exec"

// hideChildWindow does nothing off Windows: no console to hide.
func hideChildWindow(*exec.Cmd) {}
