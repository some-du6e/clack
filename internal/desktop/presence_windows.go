package desktop

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// The session's lock state, which Electron's powerMonitor reports as
// lock-screen and unlock-screen (WM_WTSSESSION_CHANGE); Clack reads it
// when asked: WTSQuerySessionInformationW(WTSSessionInfoEx)'s
// SessionFlags.

var (
	wtsapi32                        = windows.NewLazySystemDLL("wtsapi32.dll")
	procWTSQuerySessionInformationW = wtsapi32.NewProc("WTSQuerySessionInformationW")
	procWTSFreeMemory               = wtsapi32.NewProc("WTSFreeMemory")
)

const (
	wtsCurrentServerHandle = 0
	wtsCurrentSession      = 0xFFFFFFFF
	wtsSessionInfoEx       = 25 // WTS_INFO_CLASS WTSSessionInfoEx
	wtsSessionStateLock    = 0  // WTS_SESSIONSTATE_LOCK (Windows 8 and later)
)

// ScreenLocked reports whether this session's screen is locked: the
// WTSINFOEXW at level 1, whose WTSINFOEX_LEVEL1_W (8-byte aligned, after
// the DWORD Level) holds SessionId, SessionState, then SessionFlags at
// offset 16. False when Windows won't say.
func ScreenLocked() bool {
	var buf unsafe.Pointer
	var n uint32
	r, _, _ := procWTSQuerySessionInformationW.Call(wtsCurrentServerHandle, wtsCurrentSession, wtsSessionInfoEx,
		uintptr(unsafe.Pointer(&buf)), uintptr(unsafe.Pointer(&n)))
	if r == 0 || buf == nil {
		return false
	}
	defer procWTSFreeMemory.Call(uintptr(buf))
	if n < 20 || *(*uint32)(buf) != 1 {
		return false
	}
	return *(*int32)(unsafe.Add(buf, 16)) == wtsSessionStateLock
}
