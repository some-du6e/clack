package desktop

import (
	"errors"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows notification-area ("system tray") icon. Gio has no tray API,
// so this is plain Win32 on its own locked thread: a hidden window gets the
// icon's callback messages and shows the context menu.

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW       = user32.NewProc("RegisterClassExW")
	procCreateWindowExW        = user32.NewProc("CreateWindowExW")
	procDestroyWindow          = user32.NewProc("DestroyWindow")
	procDefWindowProcW         = user32.NewProc("DefWindowProcW")
	procGetMessageW            = user32.NewProc("GetMessageW")
	procTranslateMessage       = user32.NewProc("TranslateMessage")
	procDispatchMessageW       = user32.NewProc("DispatchMessageW")
	procPostMessageW           = user32.NewProc("PostMessageW")
	procSendMessageW           = user32.NewProc("SendMessageW")
	procPostQuitMessage        = user32.NewProc("PostQuitMessage")
	procRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	procLoadImageW             = user32.NewProc("LoadImageW")
	procLoadIconW              = user32.NewProc("LoadIconW")
	procGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	procCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	procAppendMenuW            = user32.NewProc("AppendMenuW")
	procTrackPopupMenu         = user32.NewProc("TrackPopupMenu")
	procDestroyMenu            = user32.NewProc("DestroyMenu")
	procGetCursorPos           = user32.NewProc("GetCursorPos")
	procSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	procShellNotifyIconW       = shell32.NewProc("Shell_NotifyIconW")
	procGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
	procSetMenuDefaultItem     = user32.NewProc("SetMenuDefaultItem")
)

var errTrayAlreadyRunning = errors.New("desktop: tray already running")

const (
	wmNull        = 0x0000
	wmDestroy     = 0x0002
	wmClose       = 0x0010
	wmCommand     = 0x0111
	wmLButtonUp   = 0x0202
	wmLButtonDbl  = 0x0203
	wmRButtonUp   = 0x0205
	wmApp         = 0x8000
	wmTrayCB      = wmApp + 1
	wmTrayTip     = wmApp + 2
	wmTrayBalloon = wmApp + 3
	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nifMessage    = 0x1
	nifIcon       = 0x2
	nifTip        = 0x4
	nifInfo       = 0x10
	niifUser      = 0x4
	niifLargeIcon = 0x20
	smCxIcon      = 11
	smCyIcon      = 12
	imageIcon     = 1
	smCxSmIcon    = 49
	smCySmIcon    = 50
	idiApp        = 32512
	mfString      = 0x0
	mfSeparator   = 0x800
	tpmRightBtn   = 0x2
	tpmReturnCmd  = 0x100
	tpmNoNotify   = 0x80
	cmdOpen       = 1
	cmdQuit       = 2
	wsOverlapped  = 0
	iconID        = 1 // the exe's icon group (packaging/windows/winres.json)
	notifyIconUID = 1
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type notifyIconData struct {
	Size            uint32
	Wnd             windows.HWND
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            windows.Handle
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUIDItem        windows.GUID
	BalloonIcon     windows.Handle
}

type point struct{ X, Y int32 }

type msg struct {
	Wnd     windows.HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

// Tray is a running tray icon.
type Tray struct {
	mu   sync.Mutex
	hwnd windows.HWND
	nid  notifyIconData
	onOpen,
	onQuit func()
	taskbarCreated uint32
	largeIcon      windows.Handle // the balloon's (SM_CXICON)
	balloon        notifyIconData // the last Balloon's NIM_MODIFY, for tests
	balloonText    [2]string      // title, content: Balloon → the tray thread
}

var (
	trayMu  sync.Mutex
	current *Tray // the wndproc callback finds its Tray here
)

// StartTray adds Clack's icon to the notification area. A left click or
// "Open Clack" calls onOpen; "Quit Clack" calls onQuit. Both run on the
// tray's thread, so post to the UI goroutine. The icon comes back if
// Explorer restarts.
func StartTray(tooltip string, onOpen, onQuit func()) (*Tray, error) {
	trayMu.Lock()
	if current != nil {
		trayMu.Unlock()
		return nil, errTrayAlreadyRunning
	}
	t := &Tray{onOpen: onOpen, onQuit: onQuit}
	current = t
	trayMu.Unlock()
	errc := make(chan error, 1)
	go t.run(tooltip, errc)
	if err := <-errc; err != nil {
		trayMu.Lock()
		current = nil
		trayMu.Unlock()
		return nil, err
	}
	return t, nil
}

func (t *Tray) run(tooltip string, errc chan<- error) {
	runtime.LockOSThread() // window messages arrive on the creating thread
	inst, _, _ := procGetModuleHandleW.Call(0)
	cls, _ := windows.UTF16PtrFromString("ClackTrayWindow")
	wc := wndClassEx{
		WndProc:   syscall.NewCallback(trayWndProc),
		Instance:  windows.Handle(inst),
		ClassName: cls,
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		errc <- err
		return
	}
	// A hidden top-level window rather than a message-only one: those get
	// neither the TaskbarCreated broadcast nor the foreground the context
	// menu needs to close when you click elsewhere.
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(cls)),
		wsOverlapped, 0, 0, 0, 0, 0, 0, inst, 0)
	if hwnd == 0 {
		errc <- err
		return
	}
	tc, _ := windows.UTF16PtrFromString("TaskbarCreated")
	r, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(tc)))
	cx, _, _ := procGetSystemMetrics.Call(smCxSmIcon)
	cy, _, _ := procGetSystemMetrics.Call(smCySmIcon)
	icon, _, _ := procLoadImageW.Call(inst, iconID, imageIcon, cx, cy, 0)
	if icon == 0 { // built without the resource: the generic app icon
		icon, _, _ = procLoadIconW.Call(0, idiApp)
	}
	lx, _, _ := procGetSystemMetrics.Call(smCxIcon)
	ly, _, _ := procGetSystemMetrics.Call(smCyIcon)
	large, _, _ := procLoadImageW.Call(inst, iconID, imageIcon, lx, ly, 0)
	if large == 0 {
		large, _, _ = procLoadIconW.Call(0, idiApp)
	}
	t.mu.Lock()
	t.hwnd = windows.HWND(hwnd)
	t.taskbarCreated = uint32(r)
	t.nid.Size = uint32(unsafe.Sizeof(t.nid))
	t.nid.Wnd = windows.HWND(hwnd)
	t.nid.ID = notifyIconUID
	t.nid.Flags = nifMessage | nifIcon | nifTip
	t.nid.CallbackMessage = wmTrayCB
	t.nid.Icon = windows.Handle(icon)
	t.largeIcon = windows.Handle(large)
	setTip(&t.nid, tooltip)
	t.mu.Unlock()
	if ok, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&t.nid))); ok == 0 {
		procDestroyWindow.Call(hwnd)
		errc <- err
		return
	}
	errc <- nil

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	trayMu.Lock()
	current = nil
	trayMu.Unlock()
}

func setTip(nid *notifyIconData, s string) {
	u, _ := windows.UTF16FromString(s)
	if len(u) > len(nid.Tip) {
		u = u[:len(nid.Tip)-1]
		u = append(u, 0)
	}
	nid.Tip = [128]uint16{}
	copy(nid.Tip[:], u)
}

// SetTooltip changes the hover text (e.g. the unread summary).
func (t *Tray) SetTooltip(s string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	setTip(&t.nid, s)
	hwnd := t.hwnd
	t.mu.Unlock()
	procPostMessageW.Call(uintptr(hwnd), wmTrayTip, 0, 0)
}

var errBalloon = errors.New("desktop: Shell_NotifyIcon couldn't show the balloon")

// Balloon shows a notification from the icon, as Electron's
// Tray.displayBalloon does on Windows (shell/browser/ui/win/notify_icon.cc
// NotifyIcon::DisplayBalloon with Electron's defaults: iconType custom,
// largeIcon): NIM_MODIFY with NIF_INFO, the title and text, uTimeout 0,
// NIIF_USER | NIIF_LARGE_ICON and the app's icon at SM_CXICON as
// hBalloonIcon. Windows 10 and 11 show it as a toast from the app. Slack
// shows its RunFromTray and WindowFlash notices this way. Any goroutine;
// it waits for the tray thread.
func (t *Tray) Balloon(title, content string) error {
	if t == nil {
		return errors.New("desktop: no tray")
	}
	t.mu.Lock()
	t.balloonText = [2]string{title, content}
	hwnd := t.hwnd
	t.mu.Unlock()
	if r, _, _ := procSendMessageW.Call(uintptr(hwnd), wmTrayBalloon, 0, 0); r == 0 {
		return errBalloon
	}
	return nil
}

// balloonData is the NOTIFYICONDATA a balloon sends. t.mu held.
func (t *Tray) balloonData() notifyIconData {
	var n notifyIconData
	n.Size = uint32(unsafe.Sizeof(n))
	n.Wnd = t.nid.Wnd
	n.ID = t.nid.ID
	n.Flags = nifInfo
	putUTF16(n.InfoTitle[:], t.balloonText[0])
	putUTF16(n.Info[:], t.balloonText[1])
	n.Version = 0 // uTimeout
	n.InfoFlags = niifUser | niifLargeIcon
	n.BalloonIcon = t.largeIcon
	return n
}

// putUTF16 copies s into dst, cut to fit with its NUL (wcslcpy).
func putUTF16(dst []uint16, s string) {
	u, _ := windows.UTF16FromString(s)
	if len(u) > len(dst) {
		u = append(u[:len(dst)-1], 0)
	}
	copy(dst, u)
}

// Close removes the icon and stops the tray thread.
func (t *Tray) Close() {
	if t == nil {
		return
	}
	t.mu.Lock()
	hwnd := t.hwnd
	t.mu.Unlock()
	procPostMessageW.Call(uintptr(hwnd), wmClose, 0, 0)
}

func trayWndProc(hwnd, m, wp, lp uintptr) uintptr {
	trayMu.Lock()
	t := current
	trayMu.Unlock()
	if t == nil {
		r, _, _ := procDefWindowProcW.Call(hwnd, m, wp, lp)
		return r
	}
	switch uint32(m) {
	case wmTrayCB:
		switch uint32(lp) & 0xffff {
		case wmLButtonUp, wmLButtonDbl:
			if t.onOpen != nil {
				t.onOpen()
			}
		case wmRButtonUp:
			t.menu(hwnd)
		}
		return 0
	case wmTrayTip:
		t.mu.Lock()
		procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&t.nid)))
		t.mu.Unlock()
		return 0
	case wmTrayBalloon:
		t.mu.Lock()
		t.balloon = t.balloonData()
		n := t.balloon
		t.mu.Unlock()
		ok, _, _ := procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&n)))
		return ok
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		t.mu.Lock()
		procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&t.nid)))
		t.mu.Unlock()
		procPostQuitMessage.Call(0)
		return 0
	}
	if t.taskbarCreated != 0 && uint32(m) == t.taskbarCreated { // Explorer restarted
		t.mu.Lock()
		procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&t.nid)))
		t.mu.Unlock()
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, m, wp, lp)
	return r
}

func (t *Tray) menu(hwnd uintptr) {
	h, _, _ := procCreatePopupMenu.Call()
	if h == 0 {
		return
	}
	defer procDestroyMenu.Call(h)
	open, _ := windows.UTF16PtrFromString("Open Clack")
	quit, _ := windows.UTF16PtrFromString("Quit Clack")
	procAppendMenuW.Call(h, mfString, cmdOpen, uintptr(unsafe.Pointer(open)))
	procAppendMenuW.Call(h, mfSeparator, 0, 0)
	procAppendMenuW.Call(h, mfString, cmdQuit, uintptr(unsafe.Pointer(quit)))
	procSetMenuDefaultItem.Call(h, cmdOpen, 0)
	var p point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	// The owner must be foreground or the menu won't close on an outside
	// click; the WM_NULL afterwards is the documented workaround (KB135788).
	procSetForegroundWindow.Call(hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(h, tpmRightBtn|tpmReturnCmd|tpmNoNotify, uintptr(p.X), uintptr(p.Y), 0, hwnd, 0)
	procPostMessageW.Call(hwnd, wmNull, 0, 0)
	switch cmd {
	case cmdOpen:
		if t.onOpen != nil {
			t.onOpen()
		}
	case cmdQuit:
		if t.onQuit != nil {
			t.onQuit()
		}
	}
}
