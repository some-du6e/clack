//go:build windows

package applog

// Native crashes on Windows. The runtime's crash output (crash.go) holds
// what Go itself reports: panics and fatal errors. A fault in code that
// isn't Go went nowhere:
//
//   - On a Go thread (a DLL Go called, cgo), the runtime's last vectored
//     continue handler prints "Exception 0xc0000005 … PC=…" and the
//     goroutines' stacks (runtime/signal_windows.go winthrow), then exits
//     with code 2. It prints to fd 2 alone (writeErrData copies to the
//     crash output only once m.dying is set, and winthrow never sets it),
//     and a windowsgui Clack has no stderr when started from the Start
//     menu, the taskbar or a link (NUL when the update helper relaunches
//     it). With no stderr, fd 2 is crash.log (STD_ERROR_HANDLE), so that
//     report lands there.
//   - On a thread Go doesn't own (a graphics driver's, the thread pool's),
//     the runtime's handlers pass (there is no g), and Windows ends the
//     process. An unhandled-exception filter, as Chromium's Crashpad
//     installs (SetUnhandledExceptionFilter), writes the exception's code,
//     where it happened (module and offset), the thread and where that
//     thread started to crash.log first, then hands the exception on to
//     the filter that was there before (mingw's CRT in a cgo build) and to
//     Windows Error Reporting.
//   - A fail-fast (a heap corruption, 0xc0000374) runs no handler at all:
//     only Windows Error Reporting sees it, and logs it (Application
//     Error, event 1000: crashevent.go). Go's runtime keeps WER out of
//     every crash at start (preventErrorDialogs: SEM_NOGPFAULTERRORBOX, so
//     Windows ends the process unrecorded; GOTRACEBACK=wer is the
//     runtime's way back, and turns every panic into a crash). armNative
//     lets WER back in (werOn), as Chromium is: Go already turned WER's
//     dialog off (WER_FAULT_REPORTING_NO_UI), so a crash is recorded, never
//     shown. CI's runners logged no crash at all before (45202224745).
//
// Clack sends nothing anywhere: crash.log stays on the disk, and the next
// start copies it, redacted, into clack.log like a Go crash. Windows Error
// Reporting treats a crash of Clack's as any app's (werOn): it records it,
// and reports it or not as the PC's settings say.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ncKernel32                    = windows.NewLazySystemDLL("kernel32.dll")
	ncNtdll                       = windows.NewLazySystemDLL("ntdll.dll")
	ncSetUnhandledExceptionFilter = ncKernel32.NewProc("SetUnhandledExceptionFilter")
	ncGetModuleHandleExW          = ncKernel32.NewProc("GetModuleHandleExW")
	ncGetErrorMode                = ncKernel32.NewProc("GetErrorMode")
	ncSetErrorMode                = ncKernel32.NewProc("SetErrorMode")
	ncNtQueryInformationThread    = ncNtdll.NewProc("NtQueryInformationThread")
)

// openCrashFile opens crash.log for this run, shared for reading only: a
// second Clack (a launch that hands its link to the running one) can't
// open it to start it afresh while the running one's crash output points
// at it.
func openCrashFile(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ, nil,
		windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return nil, errCrashFileHeld
		}
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}

// native is the native capture's state. The filter reads it while the
// process is going down, so it holds no lock there.
var native struct {
	once  sync.Once
	out   atomic.Uintptr             // crash.log (a duplicate handle) for the filter
	prev  atomic.Uintptr             // the unhandled-exception filter there before
	flush atomic.Pointer[func()]     // writes the log's queued lines
	last  atomic.Pointer[[3]uintptr] // the exception last written: code, address, thread
}

// armNative makes native faults leave their record in crash.log (f, its
// header written): the unhandled-exception filter, and fd 2 when the
// process has no stderr. It reports whether fd 2 is crash.log now (then
// the runtime's own crash output would write everything twice).
func armNative(f *os.File, flush func()) bool {
	cur := windows.CurrentProcess()
	var out windows.Handle
	if err := windows.DuplicateHandle(cur, windows.Handle(f.Fd()), cur, &out, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return false
	}
	if old := native.out.Swap(uintptr(out)); old != 0 {
		windows.CloseHandle(windows.Handle(old))
	}
	native.flush.Store(&flush)
	native.once.Do(func() {
		prev, _, _ := ncSetUnhandledExceptionFilter.Call(windows.NewCallback(unhandledFilter))
		native.prev.Store(prev)
		werOn()
	})
	if hasStderr() {
		return false
	}
	var fd2 windows.Handle
	if err := windows.DuplicateHandle(cur, windows.Handle(f.Fd()), cur, &fd2, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return false
	}
	if err := windows.SetStdHandle(windows.STD_ERROR_HANDLE, fd2); err != nil {
		windows.CloseHandle(fd2)
		return false
	}
	return true
}

// semNoGPFaultErrorBox is SetErrorMode's SEM_NOGPFAULTERRORBOX: with it,
// Windows ends a crashing process without Windows Error Reporting.
const semNoGPFaultErrorBox = 0x0002

// werOn lets Windows Error Reporting record this process's crashes: it
// clears the SEM_NOGPFAULTERRORBOX Go's runtime set at start, as the
// runtime's own enableWER does for GOTRACEBACK=wer (the other error-mode
// flags stay).
func werOn() {
	if mode, _, _ := ncGetErrorMode.Call(); mode&semNoGPFaultErrorBox != 0 {
		ncSetErrorMode.Call(mode &^ semNoGPFaultErrorBox)
	}
}

// releaseNative lets go of crash.log (tests: Windows can't remove an open
// file). The filter stays installed and writes nothing after it.
func releaseNative() {
	native.flush.Store(nil)
	if old := native.out.Swap(0); old != 0 {
		windows.CloseHandle(windows.Handle(old))
	}
}

// hasStderr reports whether fd 2 goes somewhere: a console, a pipe or a
// file. A windowsgui process started from the shell has none, and one
// started with exec.Cmd's nil Stderr has NUL (a character device that
// isn't a console).
func hasStderr() bool {
	h, err := windows.GetStdHandle(windows.STD_ERROR_HANDLE)
	if err != nil || h == 0 || h == windows.InvalidHandle {
		return false
	}
	t, err := windows.GetFileType(h)
	if err != nil {
		return false
	}
	switch t {
	case windows.FILE_TYPE_DISK, windows.FILE_TYPE_PIPE:
		return true
	case windows.FILE_TYPE_CHAR:
		var mode uint32
		return windows.GetConsoleMode(h, &mode) == nil
	}
	return false
}

// ncRecord is EXCEPTION_RECORD (Go's alignment pads NParams on 64-bit as C
// does).
type ncRecord struct {
	Code    uint32
	Flags   uint32
	Next    uintptr
	Address uintptr
	NParams uint32
	Info    [15]uintptr
}

// ncPointers is EXCEPTION_POINTERS.
type ncPointers struct {
	Record  *ncRecord
	Context uintptr
}

// unhandledFilter is the top-level exception filter: Windows calls it on
// the faulting thread for an exception no handler took.
func unhandledFilter(ep *ncPointers) uintptr {
	if ep != nil && ep.Record != nil {
		writeNative(ep.Record, "the unhandled-exception filter")
	}
	if prev := native.prev.Load(); prev != 0 {
		r, _, _ := syscall.SyscallN(prev, uintptr(unsafe.Pointer(ep)))
		return r
	}
	return 0 // EXCEPTION_CONTINUE_SEARCH: Windows Error Reporting has it next
}

// writeNative writes r's record to crash.log, once per exception, after
// the log's queued lines (the last ones before the fault).
func writeNative(r *ncRecord, by string) {
	out := windows.Handle(native.out.Load())
	if out == 0 {
		return
	}
	tid := uintptr(windows.GetCurrentThreadId())
	key := [3]uintptr{uintptr(r.Code), r.Address, tid}
	if last := native.last.Swap(&key); last != nil && *last == key {
		return
	}
	if f := native.flush.Load(); f != nil {
		(*f)()
	}
	var n uint32 // synchronous WriteFile needs the count's address
	_ = windows.WriteFile(out, []byte(nativeRecord(r, tid, by)), &n, nil)
}

// nativeRecord is the line for r, raised on thread tid.
func nativeRecord(r *ncRecord, tid uintptr, by string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s native exception 0x%08x (%s", time.Now().Format("2006/01/02 15:04:05.000"), r.Code, ncCodeName(r.Code))
	if (r.Code == 0xc0000005 || r.Code == 0xc0000006) && r.NParams >= 2 {
		fmt.Fprintf(&b, ", %s 0x%x", ncAccess(r.Info[0]), r.Info[1])
	}
	fmt.Fprintf(&b, ") at 0x%x%s on thread %d", r.Address, ncWhere(r.Address), tid)
	if s := ncThreadStart(); s != 0 {
		fmt.Fprintf(&b, ", which started at 0x%x%s", s, ncWhere(s))
	}
	fmt.Fprintf(&b, "; seen by %s\n", by)
	return b.String()
}

// ncWhere is " = module.dll+0xoffset" for addr, or " (in no module)".
func ncWhere(addr uintptr) string {
	const fromAddress, unchangedRefcount = 0x4, 0x2
	var mod windows.Handle
	if r, _, _ := ncGetModuleHandleExW.Call(fromAddress|unchangedRefcount, addr, uintptr(unsafe.Pointer(&mod))); r == 0 || mod == 0 {
		return " (in no module)"
	}
	var buf [windows.MAX_PATH]uint16
	name := "?"
	if n, err := windows.GetModuleFileName(mod, &buf[0], uint32(len(buf))); err == nil && n > 0 {
		name = filepath.Base(windows.UTF16ToString(buf[:n]))
	}
	return fmt.Sprintf(" = %s+0x%x", name, addr-uintptr(mod))
}

// ncThreadStart is where the current thread started (its Win32 start
// address), 0 if Windows won't say.
func ncThreadStart() uintptr {
	const threadQuerySetWin32StartAddress = 9
	var start uintptr
	if st, _, _ := ncNtQueryInformationThread.Call(uintptr(windows.CurrentThread()), threadQuerySetWin32StartAddress,
		uintptr(unsafe.Pointer(&start)), unsafe.Sizeof(start), 0); st != 0 {
		return 0
	}
	return start
}

func ncAccess(op uintptr) string {
	switch op {
	case 0:
		return "reading"
	case 1:
		return "writing"
	case 8:
		return "executing"
	}
	return fmt.Sprintf("operation %d at", op)
}

func ncCodeName(code uint32) string {
	switch code {
	case 0xc0000005:
		return "access violation"
	case 0xc0000006:
		return "in-page error"
	case 0xc0000008:
		return "invalid handle"
	case 0xc0000017:
		return "out of memory"
	case 0xc000001d:
		return "illegal instruction"
	case 0xc0000025:
		return "noncontinuable exception"
	case 0xc000008c:
		return "array bounds exceeded"
	case 0xc0000094:
		return "integer divide by zero"
	case 0xc0000095:
		return "integer overflow"
	case 0xc0000096:
		return "privileged instruction"
	case 0xc00000fd:
		return "stack overflow"
	case 0xc0000374:
		return "heap corruption"
	case 0xc0000409:
		return "stack buffer overrun"
	case 0xc0000420:
		return "assertion failure"
	case 0xe06d7363:
		return "C++ exception"
	case 0x80000003:
		return "breakpoint"
	}
	return "an exception"
}
