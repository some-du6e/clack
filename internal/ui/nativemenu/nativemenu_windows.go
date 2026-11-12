//go:build windows

package nativemenu

import (
	"image"
	"unsafe"

	"gioui.org/app"
	"gioui.org/io/event"
	"golang.org/x/sys/windows"
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	procDestroyMenu      = user32.NewProc("DestroyMenu")
	procAppendMenuW      = user32.NewProc("AppendMenuW")
	procTrackPopupMenuEx = user32.NewProc("TrackPopupMenuEx")
	procClientToScreen   = user32.NewProc("ClientToScreen")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procSetForegroundWnd = user32.NewProc("SetForegroundWindow")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
)

const (
	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfSeparator = 0x0800

	tpmLeftAlign   = 0x0000
	tpmTopAlign    = 0x0000
	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100
	tpmNoNotify    = 0x0080

	wmNull = 0x0000

	smMenuDropAlignment = 40
)

type point struct{ X, Y int32 }

func noteEvent(ev event.Event) {
	if e, ok := ev.(app.Win32ViewEvent); ok {
		mu.Lock()
		view = e.HWND
		mu.Unlock()
	}
}

func supported() bool { return Fake != nil || view != 0 && runner != nil }

func popup(hwnd uintptr, run func(func()), items []Item, pt image.Point, done func(int)) {
	if Fake != nil {
		done(Fake(items, pt))
		return
	}
	pick := -1
	run(func() { pick = track(hwnd, items, pt) })
	done(pick)
}

// track builds the menu and runs TrackPopupMenuEx on the window's thread:
// item i is command i+1, TPM_RETURNCMD returns it (0 for none). The
// owner is already the foreground window; the WM_NULL afterwards is the
// documented fix for a menu that doesn't close (KB135788).
func track(hwnd uintptr, items []Item, pt image.Point) int {
	h, _, _ := procCreatePopupMenu.Call()
	if h == 0 {
		return -1
	}
	defer procDestroyMenu.Call(h)
	for i, it := range items {
		if it.Sep {
			procAppendMenuW.Call(h, mfSeparator, 0, 0)
			continue
		}
		label := it.Label
		if it.Hint != "" {
			label += "\t" + it.Hint // the accelerator column
		}
		s, err := windows.UTF16PtrFromString(label)
		if err != nil {
			continue
		}
		flags := uintptr(mfString)
		if it.Disabled {
			flags |= mfGrayed
		}
		procAppendMenuW.Call(h, flags, uintptr(i+1), uintptr(unsafe.Pointer(s)))
	}
	p := point{X: int32(pt.X), Y: int32(pt.Y)}
	procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&p)))
	flags := uintptr(tpmLeftAlign | tpmTopAlign | tpmRightButton | tpmReturnCmd | tpmNoNotify)
	if r, _, _ := procGetSystemMetrics.Call(smMenuDropAlignment); r != 0 {
		flags |= 0x0008 // TPM_RIGHTALIGN, the user's handedness setting
	}
	procSetForegroundWnd.Call(hwnd)
	cmd, _, _ := procTrackPopupMenuEx.Call(h, flags, uintptr(p.X), uintptr(p.Y), hwnd, 0)
	procPostMessageW.Call(hwnd, wmNull, 0, 0)
	if cmd == 0 || int(cmd) > len(items) {
		return -1
	}
	return int(cmd) - 1
}
