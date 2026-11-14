//go:build darwin && cgo

package ui

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"gioui.org/app"
	"gioui.org/app/cocoatest"

	"github.com/some-du6e/clack/internal/ui/menubar"
)

// AppKit's menus belong to the main thread: TestMain keeps the main
// goroutine on it and runs onMain's functions there while the tests run.
// With CLACK_MACOS_WINDOW_TESTS (ci's macos job, window_darwin_test.go) it
// runs AppKit's own loop there instead, app.Main as cmd/clack's app.Events
// does, so real windows open; onMain then goes through the main queue.

func init() { runtime.LockOSThread() }

var mainQueue = make(chan func())

// macWindowTests: TestMain runs app.Main (CLACK_MACOS_WINDOW_TESTS).
var macWindowTests = os.Getenv("CLACK_MACOS_WINDOW_TESTS") != ""

func TestMain(m *testing.M) {
	if macWindowTests {
		go func() { os.Exit(m.Run()) }()
		app.Main()
		return
	}
	done := make(chan int)
	go func() { done <- m.Run() }()
	for {
		select {
		case f := <-mainQueue:
			f()
		case code := <-done:
			os.Exit(code)
		}
	}
}

func onMain(f func()) {
	if macWindowTests {
		cocoatest.OnMain(f)
		return
	}
	ran := make(chan struct{})
	mainQueue <- func() { f(); close(ran) }
	<-ran
}

// wantNative is what AppKit should hold for the model: each item's title,
// key equivalent, action and target as menubar_darwin.m's dump writes them.
func wantNative(menus []menubar.Menu) string {
	var b strings.Builder
	var walk func(items []menubar.Item, depth int)
	walk = func(items []menubar.Item, depth int) {
		for _, it := range items {
			b.WriteString(strings.Repeat("\t", depth))
			if it.Sep {
				if it.Recent {
					b.WriteString("--- hidden\n")
				} else {
					b.WriteString("---\n")
				}
				continue
			}
			act, tgt := "clackItem:", "ClackMenuTarget ok"
			switch {
			case it.Role == "services" || it.Sub != nil:
				act, tgt = "", "nil"
			case strings.HasPrefix(it.Role, "clack"):
				act = it.Role
			case it.Role != "":
				act, tgt = it.Role, "nil ok"
			}
			sub := ""
			if it.Sub != nil || it.Role == "services" {
				sub = " >"
			}
			b.WriteString(it.Title + "|" + it.Accel() + "|" + act + "|" + tgt + sub + "\n")
			walk(it.Sub, depth+1)
		}
	}
	for _, m := range menus {
		b.WriteString(m.Title + "|||nil >\n")
		walk(m.Items, 1)
	}
	return b.String()
}

// The menu bar builds in AppKit as the model says, every responder-chain
// action has an implementation (NSApplication, NSWindow, or the edit
// actions added to Gio's view), and NSApp knows its Window, Help and
// Services menus.
func TestNativeMenuBarBuilds(t *testing.T) {
	menus := testMenus()
	var got string
	onMain(func() { got = menubar.InstallNow(menus, func(string) {}) })
	want := wantNative(menus) + "windowsMenu=Window helpMenu=Help servicesMenu=Services\n"
	if got != want {
		t.Errorf("NSApp.mainMenu:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, "MISSING") {
		t.Error("an action has no implementation")
	}
	t.Logf("NSApp.mainMenu:\n%s", got)
}
