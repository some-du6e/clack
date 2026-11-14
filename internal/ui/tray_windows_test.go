//go:build windows

package ui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"
	"golang.org/x/sys/windows"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/desktop"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

var trayTestIsZoomed = trayTestUser32.NewProc("IsZoomed")

// recTray is the real desktop.Tray with what the UI asked of it recorded.
type recTray struct {
	real  *desktop.Tray
	mu    sync.Mutex
	calls []string
	menu  []desktop.TrayMenuItem
}

func (r *recTray) Balloon(title, content string) error { return r.real.Balloon(title, content) }
func (r *recTray) SetImage(s desktop.TrayState)        { r.add("image " + string(s)); r.real.SetImage(s) }
func (r *recTray) SetTooltip(s string)                 { r.add("tip " + s); r.real.SetTooltip(s) }
func (r *recTray) SetMenu(items []desktop.TrayMenuItem) {
	r.mu.Lock()
	r.menu = items
	r.mu.Unlock()
	r.real.SetMenu(items)
}

func (r *recTray) add(c string) {
	r.mu.Lock()
	r.calls = append(r.calls, c)
	r.mu.Unlock()
}

func (r *recTray) saw(c string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range r.calls {
		if x == c {
			return true
		}
	}
	return false
}

func (r *recTray) items() []desktop.TrayMenuItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.menu
}

// TestTrayWindow is the Windows shell around App.Run's own window on a
// real desktop (CLACK_WINDOWS_TRAY_TESTS, ci's windows-shell job), signed
// in to internal/fakeslack, with a real desktop.Tray:
//
//  1. launched on login (InvokedOnStartup) with Keep in Background, the
//     window starts hidden (Gio patch 23);
//  2. the tray gets Slack's highlight icon and "You have 2 notifications"
//     for two mentions, through the real Shell_NotifyIcon;
//  3. the menu's workspace row (FOREGROUND_APP) shows the window;
//  4. slack://app/?action=reset-window moves it to the cursor's display
//     and maximizes it;
//  5. the jump list Windows saved for this process holds the workspace's
//     slack://open link and Move Clack to Current Display;
//  6. the menu's Quit ends Run.
func TestTrayWindow(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_TRAY_TESTS") == "" {
		t.Skip("CLACK_WINDOWS_TRAY_TESTS not set")
	}
	ico := func(s desktop.TrayState) []byte {
		b, err := os.ReadFile(filepath.Join("..", "..", "assets", "icon", "clack-tray-"+string(s)+".ico"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	real, err := desktop.StartTray(TrayTooltip(0, false), desktop.TrayIcons{
		desktop.TrayRest: ico(desktop.TrayRest), desktop.TrayUnread: ico(desktop.TrayUnread), desktop.TrayHighlight: ico(desktop.TrayHighlight)}, nil)
	if err != nil {
		t.Fatalf("StartTray: %v", err)
	}
	defer real.Close()
	tray := &recTray{real: real}
	t.Cleanup(func() { _ = desktop.DeleteJumpList("") })

	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	home := t.TempDir()
	cache := filepath.Join(home, "cache")
	hwnds := make(chan uintptr, 1)
	var frames atomic.Int64
	images := imgcache.New(fs.Fetch, "", nil)
	a := New(Options{ConfigDir: home, NoRealtime: true, Started: time.Now(), InvokedOnStartup: true, OnWindowEvent: func(e event.Event) {
		switch e := e.(type) {
		case app.Win32ViewEvent:
			if e.HWND != 0 {
				select {
				case hwnds <- e.HWND:
				default:
				}
			}
		case app.FrameEvent:
			frames.Add(1)
		}
	}}, images)
	a.Attach(core.New(slack.New(fs.Creds()), store.New(cache)), cache)
	a.SetTray(tray)
	done := make(chan error, 1)
	since := time.Now().Add(-2 * time.Second)
	go func() { done <- a.Run() }()
	var hwnd uintptr
	select {
	case hwnd = <-hwnds:
	case <-time.After(60 * time.Second):
		t.Fatal("no Win32ViewEvent")
	}
	until := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(trayTestWaitTimeout)
		for !ok() {
			select {
			case err := <-done:
				t.Fatalf("%s: Run returned (%v)", what, err)
			default:
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	onUI := func(f func()) {
		ran := make(chan struct{})
		a.Post(func() { f(); close(ran) })
		select {
		case <-ran:
		case <-time.After(10 * time.Second):
			t.Fatal("posted work didn't run")
		}
	}
	visible := func() bool { r, _, _ := trayTestIsVisible.Call(hwnd); return r != 0 }
	zoomed := func() bool { r, _, _ := trayTestIsZoomed.Call(hwnd); return r != 0 }

	// 1. Keep in Background
	time.Sleep(3 * time.Second)
	if visible() {
		t.Fatal("launched on login with Keep in Background: the window is visible")
	}
	t.Logf("hidden; %d frames so far (Gio's configure draws them)", frames.Load())

	// 2. the icon and tooltip for two mentions
	onUI(func() {
		a.Core().SetExperimentGroup("activity_inbox", "off")
		m := a.model()
		m.HomeCount, m.HomeUnread = 2, true
		a.updateDock()
	})
	until("the highlight icon", func() bool {
		return tray.saw("image highlight") && tray.saw("tip You have 2 notifications")
	})
	uiScreenshot(t, "clack-tray-highlight")

	// 3. the workspace row brings the window out
	until("the tray menu", func() bool { return len(tray.items()) > 2 })
	row := tray.items()[0]
	if row.Sep || row.Sub != nil || row.Do == nil || !tray.items()[1].Sep {
		t.Fatalf("the menu doesn't start with the workspace: %+v", tray.items())
	}
	if trayItem(tray.items(), "&Preferences") == nil || trayItem(tray.items(), "&Quit") == nil || trayItem(tray.items(), "&Launch Control") == nil {
		t.Fatalf("the menu: %s", menuText(tray.items(), ""))
	}
	t.Logf("the tray menu:\n%s", menuText(tray.items(), ""))
	f0 := frames.Load()
	row.Do()
	until("the window shown by the workspace row", visible)
	until("a frame", func() bool { return frames.Load() > f0 })

	// 4. reset-window: the cursor's display, maximized
	a.OpenURL(resetWindowLink)
	until("the window maximized by reset-window", func() bool { return visible() && zoomed() })
	uiScreenshot(t, "clack-reset-window")

	// 5. the jump list Windows saved for this process
	until("the workspace", func() bool {
		var id string
		onUI(func() { id, _ = a.noteTeam() })
		return id != ""
	})
	var team string
	onUI(func() { team, _ = a.noteTeam() })
	open, move := utf16bytes(openTeamLink(team)), utf16bytes("Move Clack to Current Display")
	dir := filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Recent", "CustomDestinations")
	until("the saved jump list", func() bool {
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			fi, err := e.Info()
			if err != nil || fi.ModTime().Before(since) || !strings.HasSuffix(e.Name(), ".customDestinations-ms") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err == nil && bytes.Contains(b, open) && bytes.Contains(b, move) && bytes.Contains(b, utf16bytes(resetWindowLink)) {
				return true
			}
		}
		return false
	})

	// 6. Quit from the menu
	trayItem(tray.items(), "&Quit").Do()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the menu's Quit didn't end Run")
	}
}

// utf16bytes is s in UTF-16LE, as the shell's files store it.
func utf16bytes(s string) []byte {
	u := windows.StringToUTF16(s)
	b := make([]byte, 0, 2*len(u))
	for _, c := range u[:len(u)-1] {
		b = append(b, byte(c), byte(c>>8))
	}
	return b
}
