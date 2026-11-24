package openurl

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// revealAttr is explorer /select,"<path>" as its command line: Explorer
// parses its own, and wants the path quoted after the comma rather than
// the whole argument quoted as Go would.
func revealAttr(p string) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CmdLine: `explorer /select,"` + p + `"`}
}

// shellOpen is ShellExecuteW(NULL, "open", p, NULL, NULL, SW_SHOWNORMAL).
func shellOpen(p string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}
