//go:build windows

package desktop

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/some-du6e/clack/internal/desktop/uiatest"
)

// The notification-area icon's states, menu and clicks (tray_windows.go).
// TestTrayIconFromICO and TestTrayMenuItems need no shell; TestTrayShell
// (CLACK_WINDOWS_TRAY_TESTS, ci's windows-shell job) runs the icon on the
// runner's taskbar and drives it through UI Automation.

var (
	ttGetMenuItemCount = user32.NewProc("GetMenuItemCount")
	ttGetMenuItemInfoW = user32.NewProc("GetMenuItemInfoW")
	ttGetObjectW       = gdi32.NewProc("GetObjectW")
)

func trayShellTests(t *testing.T) {
	t.Helper()
	if os.Getenv("CLACK_WINDOWS_TRAY_TESTS") == "" {
		t.Skip("CLACK_WINDOWS_TRAY_TESTS not set")
	}
}

// trayICO is one of assets/icon's tray .ico files.
func trayICO(t *testing.T, state TrayState) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "assets", "icon", "clack-tray-"+string(state)+".ico"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// icoEntryBGRA is the .ico's n-px DIB entry as top-down BGRA.
func icoEntryBGRA(t *testing.T, ico []byte, n int) []byte {
	t.Helper()
	count := int(binary.LittleEndian.Uint16(ico[4:]))
	for i := 0; i < count; i++ {
		e := ico[6+16*i:]
		w := int(e[0])
		if w == 0 {
			w = 256
		}
		if w != n {
			continue
		}
		off := binary.LittleEndian.Uint32(e[12:])
		dib := ico[off:]
		hs := binary.LittleEndian.Uint32(dib)
		px := dib[hs : hs+uint32(n*n*4)]
		out := make([]byte, len(px))
		for y := 0; y < n; y++ { // bottom-up in the file
			copy(out[y*n*4:(y+1)*n*4], px[(n-1-y)*n*4:(n-y)*n*4])
		}
		return out
	}
	t.Fatalf("no %d px entry", n)
	return nil
}

type bitmapInfo struct {
	Type, Width, Height, WidthBytes int32
	Planes, BitsPixel               uint16
	Bits                            uintptr
}

// hiconBGRA reads an HICON's colour bitmap back as top-down BGRA.
func hiconBGRA(t *testing.T, icon windows.Handle) (int, []byte) {
	t.Helper()
	var ii iconInfo
	if r, _, err := ttGetIconInfo.Call(uintptr(icon), uintptr(unsafe.Pointer(&ii))); r == 0 {
		t.Fatalf("GetIconInfo: %v", err)
	}
	defer procDeleteObject.Call(uintptr(ii.Color))
	defer procDeleteObject.Call(uintptr(ii.Mask))
	var bm bitmapInfo
	ttGetObjectW.Call(uintptr(ii.Color), unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm)))
	n := int(bm.Width)
	hdr := bitmapV5Header{Width: int32(n), Height: -int32(n), Planes: 1, BitCount: 32}
	hdr.Size = 40
	px := make([]byte, n*n*4)
	dc, _, _ := ttGetDC.Call(0)
	defer ttReleaseDC.Call(0, dc)
	if r, _, err := ttGetDIBits.Call(dc, uintptr(ii.Color), 0, uintptr(n), uintptr(unsafe.Pointer(&px[0])), uintptr(unsafe.Pointer(&hdr)), 0); int(r) != n {
		t.Fatalf("GetDIBits: %d %v", r, err)
	}
	return n, px
}

// iconFromICO is LoadImage from the .ico at a size: for each size Slack's
// tray icons carry (SM_CXSMICON at 100, 125, 150 and 200%) the entry of
// that size, pixel for pixel; a size without one is the nearest scaled.
func TestTrayIconFromICO(t *testing.T) {
	for _, s := range []TrayState{TrayRest, TrayUnread, TrayHighlight} {
		ico := trayICO(t, s)
		for _, n := range []int{16, 20, 24, 32} {
			h, err := iconFromICO(ico, n)
			if err != nil {
				t.Fatalf("%s %d: %v", s, n, err)
			}
			got, px := hiconBGRA(t, h)
			procDestroyIcon.Call(uintptr(h))
			if got != n {
				t.Fatalf("%s %d: the icon is %d px", s, n, got)
			}
			want := icoEntryBGRA(t, ico, n)
			for i := 0; i < len(want); i += 4 {
				if px[i] != want[i] || px[i+1] != want[i+1] || px[i+2] != want[i+2] || px[i+3] != want[i+3] {
					t.Fatalf("%s %d: pixel %d is %v, the .ico's %v", s, n, i/4, px[i:i+4], want[i:i+4])
				}
			}
		}
		h, err := iconFromICO(ico, 40)
		if err != nil {
			t.Fatalf("%s 40: %v", s, err)
		}
		if got, _ := hiconBGRA(t, h); got != 40 {
			t.Errorf("%s 40: %d px, want the 32 px entry scaled to 40", s, got)
		}
		procDestroyIcon.Call(uintptr(h))
	}
	if _, err := iconFromICO([]byte("not an icon"), 16); err == nil {
		t.Error("iconFromICO took bytes that aren't an .ico")
	}
}

// menuDump is buildMenu's menu read back with GetMenuItemInfoW: one line
// per item, "(o)" a checked radio item, "[x]"/"[ ]" a checked/unchecked
// plain item's check, "~" greyed, "-" a separator, "#n" the command.
func menuDump(t *testing.T, h uintptr, indent string) string {
	t.Helper()
	n, _, _ := ttGetMenuItemCount.Call(h)
	var b strings.Builder
	for i := 0; i < int(int32(n)); i++ {
		buf := make([]uint16, 256)
		mi := menuItemInfo{Mask: miimFType | miimState | miimID | miimString | miimSubmenu, TypeData: &buf[0], Cch: uint32(len(buf))}
		mi.Size = uint32(unsafe.Sizeof(mi))
		if r, _, err := ttGetMenuItemInfoW.Call(h, uintptr(i), 1, uintptr(unsafe.Pointer(&mi))); r == 0 {
			t.Fatalf("GetMenuItemInfoW(%d): %v", i, err)
		}
		if mi.Type&mftSeparator != 0 {
			b.WriteString(indent + "-\n")
			continue
		}
		mark := ""
		switch {
		case mi.Type&mftRadio != 0 && mi.State&mfsChecked != 0:
			mark = "(o) "
		case mi.Type&mftRadio != 0:
			mark = "( ) "
		case mi.State&mfsChecked != 0:
			mark = "[x] "
		}
		grey := ""
		if mi.State&mfsDisabled != 0 {
			grey = "~"
		}
		label := strings.ReplaceAll(windows.UTF16ToString(buf), "\t", " <tab> ")
		if mi.SubMenu != 0 {
			fmt.Fprintf(&b, "%s%s%s%s >\n%s", indent, grey, mark, label, menuDump(t, mi.SubMenu, indent+"  "))
			continue
		}
		fmt.Fprintf(&b, "%s%s%s%s #%d\n", indent, grey, mark, label, mi.ID)
	}
	return b.String()
}

// buildMenu makes Electron's item types as a native menu: labels with
// their mnemonics and accelerators, a checkbox, radio items checked and
// not, greyed plain items, separators and a submenu, and each command's
// number runs that item.
func TestTrayMenuItems(t *testing.T) {
	ran := map[string]int{}
	do := func(k string) func() { return func() { ran[k]++ } }
	items := []TrayMenuItem{
		{Label: "Fake Workspace", Do: do("team")},
		{Sep: true},
		{Label: "&Launch Control", Sub: []TrayMenuItem{
			{Label: "&Launch on Login", Check: true, Checked: true, Do: do("login")},
			{Sep: true},
			{Label: "Show Clack &Immediately", Radio: true, Checked: false, Do: do("show")},
			{Label: "Keep in &Background", Radio: true, Checked: true, Do: do("bg")},
		}},
		{Label: "&Launch Control", Sub: []TrayMenuItem{
			{Label: "&Launch on Login", Check: true, Do: do("login2")},
			{Label: "Keep in &Background", Checked: true, Disabled: true, Do: do("bg2")},
		}},
		{Label: "&Preferences", Accel: "Ctrl+,", Do: do("prefs")},
		{Label: "&Quit", Accel: "Ctrl+Q", Do: do("quit")},
	}
	var cmds []func()
	h, err := buildMenu(items, &cmds)
	if err != nil {
		t.Fatal(err)
	}
	defer procDestroyMenu.Call(h)
	got := menuDump(t, h, "")
	want := `Fake Workspace #1
-
&Launch Control >
  [x] &Launch on Login #2
  -
  ( ) Show Clack &Immediately #3
  (o) Keep in &Background #4
&Launch Control >
  &Launch on Login #5
  ~Keep in &Background #6
&Preferences <tab> Ctrl+, #7
&Quit <tab> Ctrl+Q #8
`
	if got != want {
		t.Fatalf("menu\n%s\nwant\n%s", got, want)
	}
	if len(cmds) != 8 {
		t.Fatalf("%d commands", len(cmds))
	}
	for i, k := range []string{"team", "login", "show", "bg", "login2", "bg2", "prefs", "quit"} {
		cmds[i]()
		if ran[k] != 1 {
			t.Errorf("command %d ran %v", i+1, ran)
		}
	}
}

// The icon on the runner's notification area: StartTray adds it, SetImage
// swaps the state icons (NIM_MODIFY with the .ico's HICON for
// SM_CXSMICON), the tooltip reaches the shell (UI Automation finds the
// icon by it, in the overflow if Windows put it there), a left button
// press is the click, and a right one opens the menu, whose item UI
// Automation invokes.
func TestTrayShell(t *testing.T) {
	trayShellTests(t)
	clicks := make(chan struct{}, 4)
	icons := TrayIcons{TrayRest: trayICO(t, TrayRest), TrayUnread: trayICO(t, TrayUnread), TrayHighlight: trayICO(t, TrayHighlight)}
	tr, err := StartTray("No unread messages", icons, func() { clicks <- struct{}{} })
	if err != nil {
		t.Fatalf("StartTray: %v", err)
	}
	defer tr.Close()
	cx, _, _ := procGetSystemMetrics.Call(smCxSmIcon)
	t.Logf("SM_CXSMICON %d", cx)
	shown := func() windows.Handle {
		tr.mu.Lock()
		defer tr.mu.Unlock()
		return tr.nid.Icon
	}
	for _, s := range []TrayState{TrayUnread, TrayHighlight, TrayRest} {
		tr.SetImage(s)
		deadline := time.Now().Add(5 * time.Second)
		for {
			tr.mu.Lock()
			h, ok := tr.hicons[s]
			tr.mu.Unlock()
			if ok && shown() == h {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: the tray's icon is %#x", s, shown())
			}
			time.Sleep(20 * time.Millisecond)
		}
		tr.mu.Lock()
		h := tr.hicons[s]
		tr.mu.Unlock()
		n, px := hiconBGRA(t, h)
		want := icoEntryBGRA(t, icons[s], n)
		if string(px) != string(want) {
			t.Errorf("%s: the icon's %d px pixels aren't the .ico's", s, n)
		}
	}

	tip := "You have 3 notifications"
	tr.SetTooltip(tip)
	tr.SetImage(TrayHighlight)
	isIcon := func(e uiatest.Element) bool { return strings.Contains(e.Name, tip) }
	el, err := uiatest.Find(isIcon, 5*time.Second)
	if err != nil {
		// Windows 11 puts a new icon in the overflow: open it
		if o, err := uiatest.Act(func(e uiatest.Element) bool {
			n := strings.ToLower(e.Name)
			return strings.Contains(n, "show hidden icons") || strings.Contains(n, "notification chevron")
		}, 5*time.Second); err != nil {
			t.Logf("no overflow button: %v", err)
		} else {
			t.Logf("opened the overflow: %s", o)
		}
		el, err = uiatest.Find(isIcon, 10*time.Second)
	}
	if err != nil {
		t.Errorf("UI Automation found no icon named %q: %v\n%s", tip, err, trayDump())
	} else {
		t.Logf("the icon: %s", el)
	}
	screenshot(t, "tray-highlight")

	// a left button press is 'click'
	hwnd := uintptr(tr.hwnd)
	procPostMessageW.Call(hwnd, wmTrayCB, notifyIconUID, wmLButtonDown)
	select {
	case <-clicks:
	case <-time.After(5 * time.Second):
		t.Fatal("WM_LBUTTONDOWN: no click")
	}
	procPostMessageW.Call(hwnd, wmTrayCB, notifyIconUID, wmLButtonDbl)
	select {
	case <-clicks:
		t.Error("WM_LBUTTONDBLCLK clicked again (Electron's double-click, which Slack doesn't use)")
	case <-time.After(500 * time.Millisecond):
	}

	// a right button press opens the menu; invoking an item runs it
	var mu sync.Mutex
	var chose []string
	pick := func(k string) func() {
		return func() { mu.Lock(); chose = append(chose, k); mu.Unlock() }
	}
	tr.SetMenu([]TrayMenuItem{
		{Label: "Fake Workspace", Do: pick("team")},
		{Sep: true},
		{Label: "&Launch Control", Sub: []TrayMenuItem{{Label: "&Launch on Login", Check: true, Do: pick("login")}}},
		{Label: "&Preferences", Accel: "Ctrl+,", Do: pick("prefs")},
		{Sep: true},
		{Label: "&Quit", Accel: "Ctrl+Q", Do: pick("quit")},
	})
	procPostMessageW.Call(hwnd, wmTrayCB, notifyIconUID, wmRButtonDown)
	isItem := func(name string) func(uiatest.Element) bool {
		return func(e uiatest.Element) bool { return e.Control == 50011 && strings.HasPrefix(e.Name, name) } // UIA_MenuItemControlTypeId
	}
	if _, err := uiatest.Find(isItem("Fake Workspace"), 10*time.Second); err != nil {
		t.Fatalf("the menu didn't open: %v\n%s", err, trayDump())
	}
	screenshot(t, "tray-menu")
	if _, err := uiatest.Act(isItem("Preferences"), 5*time.Second); err != nil {
		t.Fatalf("invoking Preferences: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		got := strings.Join(chose, ",")
		mu.Unlock()
		if got == "prefs" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("items run: %q, want prefs", got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// trayDump lists the taskbar's and the menus' elements, for a failure.
func trayDump() string {
	var b strings.Builder
	n := 0
	_, _ = uiatest.Find(func(e uiatest.Element) bool {
		w := strings.ToLower(e.Window + " " + e.Class)
		if (strings.Contains(w, "taskbar") || strings.Contains(w, "tray") || strings.Contains(w, "overflow") || e.Class == "#32768") && n < 200 {
			fmt.Fprintf(&b, "  %s\n", e)
			n++
		}
		return false
	}, 0)
	return b.String()
}
