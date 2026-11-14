package desktop

import (
	"encoding/binary"
	"errors"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows notification-area ("system tray") icon, as Slack desktop's
// TrayHandler drives it (main.bundle.cjs 4.53.276 win32, class vA
// @1175804) through Electron's Tray (shell/browser/api/electron_api_tray.cc,
// shell/browser/ui/win/notify_icon.cc and notify_icon_host.cc, read at
// Electron main). Gio has no tray API, so this is plain Win32 on its own
// locked thread: a hidden window gets the icon's callback messages and
// shows the context menu.
//
//   - The image: setTrayIcon (@1178674) loads
//     resources/slack-taskbar-{rest,unread,highlight}.ico and calls
//     Tray.setImage, which on Windows is NativeImage::GetHICON at
//     GetSystemMetrics(SM_CXSMICON) (LoadImage of the .ico at that size)
//     and NotifyIcon::SetImage (NIM_MODIFY, NIF_ICON). StartTray takes the
//     .ico files, SetImage the state.
//   - The tooltip: Tray.setToolTip, NIM_MODIFY with NIF_TIP (SetTooltip).
//   - Clicks: NotifyIconHost answers the button-down messages.
//     WM_LBUTTONDOWN is 'click' (Slack's handleTrayClick: FOREGROUND_APP),
//     WM_RBUTTONDOWN and WM_CONTEXTMENU pop the context menu, and
//     WM_LBUTTONDBLCLK is 'double-click', which Slack doesn't listen to.
//   - The menu: createTrayMenu (@1179887) gives Tray.setContextMenu the
//     items, and Electron runs them in views::MenuRunner (HAS_MNEMONICS)
//     after SetForegroundWindow on its host window. SetMenu takes the
//     items; Clack shows them as a native popup menu with the same labels
//     (& before the mnemonic), checkboxes, radio items, greyed items,
//     submenus and accelerators (after a tab, right-aligned). Electron
//     gives up when SetForegroundWindow fails; Clack shows the menu
//     anyway (it then may not close on an outside click).

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW            = user32.NewProc("RegisterClassExW")
	procCreateWindowExW             = user32.NewProc("CreateWindowExW")
	procDestroyWindow               = user32.NewProc("DestroyWindow")
	procDefWindowProcW              = user32.NewProc("DefWindowProcW")
	procGetMessageW                 = user32.NewProc("GetMessageW")
	procTranslateMessage            = user32.NewProc("TranslateMessage")
	procDispatchMessageW            = user32.NewProc("DispatchMessageW")
	procPostMessageW                = user32.NewProc("PostMessageW")
	procSendMessageW                = user32.NewProc("SendMessageW")
	procPostQuitMessage             = user32.NewProc("PostQuitMessage")
	procRegisterWindowMessageW      = user32.NewProc("RegisterWindowMessageW")
	procLoadImageW                  = user32.NewProc("LoadImageW")
	procLoadIconW                   = user32.NewProc("LoadIconW")
	procGetSystemMetrics            = user32.NewProc("GetSystemMetrics")
	procCreatePopupMenu             = user32.NewProc("CreatePopupMenu")
	procInsertMenuItemW             = user32.NewProc("InsertMenuItemW")
	procTrackPopupMenu              = user32.NewProc("TrackPopupMenu")
	procDestroyMenu                 = user32.NewProc("DestroyMenu")
	procGetCursorPos                = user32.NewProc("GetCursorPos")
	procSetForegroundWindow         = user32.NewProc("SetForegroundWindow")
	procLookupIconIdFromDirectoryEx = user32.NewProc("LookupIconIdFromDirectoryEx")
	procCreateIconFromResourceEx    = user32.NewProc("CreateIconFromResourceEx")
	procShellNotifyIconW            = shell32.NewProc("Shell_NotifyIconW")
	procGetModuleHandleW            = kernel32.NewProc("GetModuleHandleW")
)

var errTrayAlreadyRunning = errors.New("desktop: tray already running")

const (
	wmNull         = 0x0000
	wmDestroy      = 0x0002
	wmClose        = 0x0010
	wmContextMenu  = 0x007B
	wmLButtonDown  = 0x0201
	wmLButtonDbl   = 0x0203
	wmRButtonDown  = 0x0204
	wmApp          = 0x8000
	wmTrayCB       = wmApp + 1
	wmTrayTip      = wmApp + 2
	wmTrayBalloon  = wmApp + 3
	wmTrayImage    = wmApp + 4
	nimAdd         = 0
	nimModify      = 1
	nimDelete      = 2
	nifMessage     = 0x1
	nifIcon        = 0x2
	nifTip         = 0x4
	nifInfo        = 0x10
	niifUser       = 0x4
	niifLargeIcon  = 0x20
	smCxIcon       = 11
	smCyIcon       = 12
	imageIcon      = 1
	smCxSmIcon     = 49
	smCySmIcon     = 50
	idiApp         = 32512
	lrDefaultColor = 0
	tpmRightBtn    = 0x2
	tpmReturnCmd   = 0x100
	tpmNoNotify    = 0x80
	wsOverlapped   = 0
	iconID         = 1 // the exe's icon group (packaging/windows/winres.json)
	notifyIconUID  = 1

	// MENUITEMINFOW
	miimState    = 0x1
	miimID       = 0x2
	miimSubmenu  = 0x4
	miimString   = 0x40
	miimFType    = 0x100
	mftSeparator = 0x800
	mftRadio     = 0x200
	mfsDisabled  = 0x3
	mfsChecked   = 0x8
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

// menuItemInfo is MENUITEMINFOW.
type menuItemInfo struct {
	Size         uint32
	Mask         uint32
	Type         uint32
	State        uint32
	ID           uint32
	SubMenu      uintptr
	CheckedBmp   uintptr
	UncheckedBmp uintptr
	ItemData     uintptr
	TypeData     *uint16
	Cch          uint32
	ItemBmp      uintptr
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

// TrayState is the tray icon's state, Slack's getTrayStateFromUnreadsInfo
// icon: the name in resources/slack-taskbar-<state>.ico.
type TrayState string

const (
	TrayRest      TrayState = "rest"      // nothing unread
	TrayUnread    TrayState = "unread"    // unread messages
	TrayHighlight TrayState = "highlight" // mentions (unread highlights)
)

// TrayIcons are the .ico files for the tray's states. A state without one
// shows the exe's own icon.
type TrayIcons map[TrayState][]byte

// TrayMenuItem is an item of the tray's context menu: the MenuItem
// options createTrayMenu passes Electron.
type TrayMenuItem struct {
	// Sep is a separator (type "separator"); nothing else is read.
	Sep bool
	// Label is the text with "&" before the mnemonic ("&Quit").
	Label string
	// Accel is the accelerator as the menu shows it after the label
	// ("Ctrl+Q").
	Accel string
	// Check and Radio are the "checkbox" and "radio" types, Checked their
	// state; a plain item ("normal") shows no mark whatever Checked says.
	Check, Radio, Checked bool
	// Disabled greys the item out (enabled: false).
	Disabled bool
	// Sub is a submenu; Do is then not used.
	Sub []TrayMenuItem
	// Do runs when the item is chosen, on the tray's thread: hand off
	// (post to the UI goroutine), don't block.
	Do func()
}

// Tray is a running tray icon.
type Tray struct {
	mu      sync.Mutex
	hwnd    windows.HWND
	nid     notifyIconData
	onClick func()
	items   []TrayMenuItem
	icons   TrayIcons
	state   TrayState
	// the tray thread's own: the state icons made so far, the exe's icon
	// (states without a file) and whether a menu is up
	hicons         map[TrayState]windows.Handle
	appIcon        windows.Handle
	inMenu         bool
	taskbarCreated uint32
	largeIcon      windows.Handle // the balloon's (SM_CXICON)
	balloon        notifyIconData // the last Balloon's NIM_MODIFY, for tests
	balloonText    [2]string      // title, content: Balloon → the tray thread
}

var (
	trayMu  sync.Mutex
	current *Tray // the wndproc callback finds its Tray here
)

// StartTray adds Clack's icon to the notification area in the rest state,
// with icons' .ico files for its states and the tooltip. A left click
// calls onClick, on the tray's thread (post to the UI goroutine). The
// context menu is empty until SetMenu. The icon comes back if Explorer
// restarts.
func StartTray(tooltip string, icons TrayIcons, onClick func()) (*Tray, error) {
	trayMu.Lock()
	if current != nil {
		trayMu.Unlock()
		return nil, errTrayAlreadyRunning
	}
	t := &Tray{onClick: onClick, icons: icons, state: TrayRest, hicons: map[TrayState]windows.Handle{}}
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
	app, _, _ := procLoadImageW.Call(inst, iconID, imageIcon, cx, cy, 0)
	if app == 0 { // built without the resource: the generic app icon
		app, _, _ = procLoadIconW.Call(0, idiApp)
	}
	lx, _, _ := procGetSystemMetrics.Call(smCxIcon)
	ly, _, _ := procGetSystemMetrics.Call(smCyIcon)
	large, _, _ := procLoadImageW.Call(inst, iconID, imageIcon, lx, ly, 0)
	if large == 0 {
		large, _, _ = procLoadIconW.Call(0, idiApp)
	}
	t.appIcon = windows.Handle(app)
	icon := t.stateIcon(TrayRest)
	t.mu.Lock()
	t.hwnd = windows.HWND(hwnd)
	t.taskbarCreated = uint32(r)
	t.nid.Size = uint32(unsafe.Sizeof(t.nid))
	t.nid.Wnd = windows.HWND(hwnd)
	t.nid.ID = notifyIconUID
	t.nid.Flags = nifMessage | nifIcon | nifTip
	t.nid.CallbackMessage = wmTrayCB
	t.nid.Icon = icon
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

// stateIcon is the HICON for state s, made from its .ico the first time
// (NativeImage::GetHICON at SM_CXSMICON); the exe's icon without one. The
// tray's thread.
func (t *Tray) stateIcon(s TrayState) windows.Handle {
	if h, ok := t.hicons[s]; ok {
		return h
	}
	ico := t.icons[s]
	if len(ico) == 0 {
		return t.appIcon
	}
	cx, _, _ := procGetSystemMetrics.Call(smCxSmIcon)
	h, err := iconFromICO(ico, int(cx))
	if err != nil {
		return t.appIcon
	}
	t.hicons[s] = h
	return h
}

// iconFromICO is LoadImage(path, IMAGE_ICON, size, size, LR_LOADFROMFILE)
// for an .ico file's bytes, which is how Electron reads an icon at a size
// (ReadICOFromPath): the entry LookupIconIdFromDirectoryEx picks for the
// size, made with CreateIconFromResourceEx (scaled to the size when no
// entry has it).
func iconFromICO(ico []byte, size int) (windows.Handle, error) {
	if len(ico) < 6 || binary.LittleEndian.Uint16(ico[2:]) != 1 {
		return 0, errors.New("desktop: not an .ico")
	}
	n := int(binary.LittleEndian.Uint16(ico[4:]))
	if n == 0 || len(ico) < 6+16*n {
		return 0, errors.New("desktop: a truncated .ico")
	}
	// The RT_GROUP_ICON directory LookupIconIdFromDirectoryEx reads: the
	// .ico's entries with an id (1…n) in place of each image's offset.
	dir := make([]byte, 6+14*n)
	copy(dir, ico[:6])
	type entry struct{ off, size uint32 }
	entries := make([]entry, n)
	for i := 0; i < n; i++ {
		e := ico[6+16*i : 6+16*(i+1)]
		d := dir[6+14*i : 6+14*(i+1)]
		copy(d[:12], e[:12])
		binary.LittleEndian.PutUint16(d[12:], uint16(i+1))
		entries[i] = entry{binary.LittleEndian.Uint32(e[12:]), binary.LittleEndian.Uint32(e[8:])}
	}
	id, _, err := procLookupIconIdFromDirectoryEx.Call(uintptr(unsafe.Pointer(&dir[0])), 1, uintptr(size), uintptr(size), lrDefaultColor)
	if id == 0 || int(id) > n {
		return 0, err
	}
	e := entries[id-1]
	if e.size == 0 || uint64(e.off)+uint64(e.size) > uint64(len(ico)) {
		return 0, errors.New("desktop: a truncated .ico")
	}
	bits := ico[e.off : e.off+e.size]
	h, _, err := procCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&bits[0])), uintptr(len(bits)), 1, 0x00030000,
		uintptr(size), uintptr(size), lrDefaultColor)
	if h == 0 {
		return 0, err
	}
	return windows.Handle(h), nil
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

// SetTooltip changes the hover text (Tray.setToolTip: Slack's "You have
// unread messages" and the like). Any goroutine.
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

// SetImage shows the icon for state s (setTrayIcon's Tray.setImage). Any
// goroutine.
func (t *Tray) SetImage(s TrayState) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.state = s
	hwnd := t.hwnd
	t.mu.Unlock()
	procPostMessageW.Call(uintptr(hwnd), wmTrayImage, 0, 0)
}

// SetMenu replaces the context menu (Tray.setContextMenu): the next right
// click shows items. Any goroutine.
func (t *Tray) SetMenu(items []TrayMenuItem) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.items = items
	t.mu.Unlock()
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
		case wmLButtonDown: // 'click' (a double click's first press too)
			if t.onClick != nil {
				t.onClick()
			}
		case wmRButtonDown, wmContextMenu:
			t.menu(hwnd)
		}
		return 0
	case wmTrayTip:
		t.mu.Lock()
		procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&t.nid)))
		t.mu.Unlock()
		return 0
	case wmTrayImage:
		t.mu.Lock()
		s := t.state
		t.mu.Unlock()
		icon := t.stateIcon(s)
		t.mu.Lock()
		t.nid.Icon = icon
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
		for s, h := range t.hicons {
			procDestroyIcon.Call(uintptr(h))
			delete(t.hicons, s)
		}
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

// menu shows the context menu at the cursor and runs the chosen item.
// The tray's thread.
func (t *Tray) menu(hwnd uintptr) {
	t.mu.Lock()
	items := t.items
	t.mu.Unlock()
	if len(items) == 0 || t.inMenu {
		return
	}
	var cmds []func()
	h, err := buildMenu(items, &cmds)
	if err != nil {
		return
	}
	defer procDestroyMenu.Call(h) // and its submenus
	var p point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	// The owner must be foreground or the menu won't close on an outside
	// click; the WM_NULL afterwards is the documented workaround (KB135788).
	procSetForegroundWindow.Call(hwnd)
	t.inMenu = true
	cmd, _, _ := procTrackPopupMenu.Call(h, tpmRightBtn|tpmReturnCmd|tpmNoNotify, uintptr(p.X), uintptr(p.Y), 0, hwnd, 0)
	t.inMenu = false
	procPostMessageW.Call(hwnd, wmNull, 0, 0)
	if cmd > 0 && int(cmd) <= len(cmds) && cmds[cmd-1] != nil {
		cmds[cmd-1]()
	}
}

// buildMenu makes the popup menu for items: each item a MENUITEMINFOW
// (MFT_RADIOCHECK for a radio item, MFS_CHECKED, MFS_DISABLED, a submenu)
// and each command, numbered from 1, the item's Do in cmds.
func buildMenu(items []TrayMenuItem, cmds *[]func()) (uintptr, error) {
	h, _, err := procCreatePopupMenu.Call()
	if h == 0 {
		return 0, err
	}
	for i, it := range items {
		mi := menuItemInfo{Mask: miimFType}
		mi.Size = uint32(unsafe.Sizeof(mi))
		var text []uint16
		if it.Sep {
			mi.Type = mftSeparator
		} else {
			label := it.Label
			if it.Accel != "" {
				label += "\t" + it.Accel
			}
			text, _ = windows.UTF16FromString(label)
			mi.Mask |= miimState | miimString
			mi.TypeData = &text[0]
			if it.Radio {
				mi.Type |= mftRadio
			}
			if it.Disabled {
				mi.State |= mfsDisabled
			}
			if (it.Check || it.Radio) && it.Checked {
				mi.State |= mfsChecked
			}
			if it.Sub != nil {
				sub, err := buildMenu(it.Sub, cmds)
				if err != nil {
					procDestroyMenu.Call(h)
					return 0, err
				}
				mi.Mask |= miimSubmenu
				mi.SubMenu = sub
			} else {
				*cmds = append(*cmds, it.Do)
				mi.Mask |= miimID
				mi.ID = uint32(len(*cmds))
			}
		}
		r, _, err := procInsertMenuItemW.Call(h, uintptr(i), 1, uintptr(unsafe.Pointer(&mi)))
		runtime.KeepAlive(text)
		if r == 0 {
			if mi.SubMenu != 0 {
				procDestroyMenu.Call(mi.SubMenu)
			}
			procDestroyMenu.Call(h)
			return 0, err
		}
	}
	return h, nil
}
