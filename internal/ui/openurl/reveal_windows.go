package openurl

import "syscall"

// revealAttr is explorer /select,"<path>" as its command line: Explorer
// parses its own, and wants the path quoted after the comma rather than
// the whole argument quoted as Go would.
func revealAttr(p string) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CmdLine: `explorer /select,"` + p + `"`}
}
