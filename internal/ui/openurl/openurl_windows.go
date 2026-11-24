package openurl

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// On Windows a link goes to the shell as Slack desktop hands it over: its
// openExternal (main.bundle.cjs, win32 4.53.276) calls Electron's
// shell.openExternal unless settings.isOpenExternalShellForced is false
// (it defaults to true there), and spawns "RunDll32.exe
// URL.dll,FileProtocolHandler <url>" only as the retry after that fails.
// Electron's shell.openExternal is platform_util_win.cc's
// OpenExternalOnWorkerThread: ShellExecuteEx with the verb "open" on the
// quoted, EscapeExternalHandlerValue'd URL, SEE_MASK_NOASYNC |
// SEE_MASK_FLAG_NO_UI, SW_SHOWNORMAL, on a COM STA thread-pool thread
// (ScopedCOMInitializer: COINIT_APARTMENTTHREADED | COINIT_DISABLE_OLE1DDE).
// Clack ran rundll32 for every link: a process whose start, DLL loads and
// exit come before the shell's own hand-off (openurl_latency_windows_test.go
// measures both).

var procShellExecuteExW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

const (
	seeMaskNoAsync  = 0x00000100 // SEE_MASK_NOASYNC
	seeMaskFlagNoUI = 0x00000400 // SEE_MASK_FLAG_NO_UI
	swShowNormal    = 1          // SW_SHOWNORMAL
)

// shellExecuteInfo is SHELLEXECUTEINFOW.
type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         windows.Handle
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     windows.Handle
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    windows.Handle
	dwHotKey     uint32
	hIcon        windows.Handle // the hIcon / hMonitor union
	hProcess     windows.Handle
}

// shellOpen hands an already-checked URL to the shell as Electron's
// OpenExternalOnWorkerThread does, on this goroutine's own locked OS
// thread in a single-threaded apartment. It returns once the shell has
// taken the URL (SEE_MASK_NOASYNC), before the handler's window is up.
func shellOpen(u string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err == nil {
		defer windows.CoUninitialize()
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(`"` + escapeExternalHandlerValue(u) + `"`)
	if err != nil {
		return err
	}
	info := shellExecuteInfo{fMask: seeMaskNoAsync | seeMaskFlagNoUI, lpVerb: verb, lpFile: file, nShow: swShowNormal}
	info.cbSize = uint32(unsafe.Sizeof(info))
	if r, _, err := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		return err
	}
	return nil
}
