//go:build !windows

package openurl

import "syscall"

// revealAttr is Windows' Explorer command line (reveal_windows.go).
func revealAttr(string) *syscall.SysProcAttr { return nil }
