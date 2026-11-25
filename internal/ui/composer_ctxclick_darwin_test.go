//go:build darwin && cgo

package ui

import (
	"image"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/app/cocoatest"

	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/nativemenu"
)

// TestMacComposerLeftClickAfterMenu is Karim's report (2026-11-24, α.165
// on Apple silicon: a left click in the composer opened its text menu,
// "Search with Google", Cut, Copy) in App.Run's own window
// (CLACK_MACOS_WINDOW_TESTS=1, ci's macos job). AppKit opens the
// composer's native menu on the right press, and the menu's tracking loop
// takes the button's release, so the view never gets it: here the release
// is never posted, and the menu (nativemenu.Fake) is dismissed. A left
// click in the composer then places the caret and opens no menu. Gio kept
// the right button down until patch 68, and the left press came as
// Primary|Secondary, a context click (ctxclick.Press): the word under it
// selected and the menu opened again.
func TestMacComposerLeftClickAfterMenu(t *testing.T) {
	if !macWindowTests {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	t.Setenv("CLACK_INSPECT", "")
	var menus atomic.Int64
	nativemenu.Fake = func([]nativemenu.Item, image.Point) int { menus.Add(1); return -1 } // dismissed
	t.Cleanup(func() { nativemenu.Fake = nil })
	ma := runMacBoundsApp(t, nil)
	defer ma.quit(t, true)
	onMain(func() { cocoatest.OrderFront(ma.view) })
	ma.until(t, "the key window", func() bool { return ma.state().Key })

	var ed *composer.Editor
	var box image.Rectangle
	ma.until(t, "the channel composer", func() bool {
		ma.onUI(t, func() {
			ch, ok := strings.CutPrefix(string(ma.a.router.Current()), "c/")
			if !ok {
				return
			}
			if cv := ma.a.convos[ch+"/"]; cv != nil && cv.comp != nil {
				if cb, ok := cv.comp.(composerBox); ok {
					if r := cb.BoxRect(); !r.Empty() {
						ed, _ = cb.FocusTag().(*composer.Editor)
						box = r
					}
				}
			}
		})
		return ed != nil
	})
	ma.onUI(t, func() { ed.InsertText("hello there world") })
	time.Sleep(500 * time.Millisecond) // its frame
	var px image.Point
	ma.onUI(t, func() { px = ma.a.winSize })
	content := ma.state().Content
	if px.X <= 0 || content.W <= 0 {
		t.Fatalf("the window is %v px, its content view %+v", px, content)
	}
	scale := float64(px.X) / content.W // Gio's px per window point
	pt := func(x, y int) (float64, float64) { return float64(x) / scale, float64(y) / scale }
	post := func(b cocoatest.MouseButton, down bool, x, y int) {
		wx, wy := pt(x, y)
		onMain(func() { cocoatest.PostButton(ma.view, b, down, wx, wy, 1) })
	}
	waitMenus := func(n int64) bool {
		for deadline := time.Now().Add(1500 * time.Millisecond); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if menus.Load() >= n {
				return true
			}
		}
		return false
	}
	// the editor's line: down the box, 30 px in (on "hello"), the first
	// point a whole right click opens the menu at
	x := box.Min.X + 30
	y := -1
	for yy := box.Min.Y + 4; yy < box.Max.Y-4 && y < 0; yy += 6 {
		n := menus.Load()
		post(cocoatest.RightButton, true, x, yy)
		post(cocoatest.RightButton, false, x, yy)
		if waitMenus(n + 1) {
			y = yy
		}
	}
	if y < 0 {
		t.Fatalf("no right click in the composer's box %v (px, %.2f px a point) opened its menu", box, scale)
	}
	time.Sleep(300 * time.Millisecond)

	// the right press, its release taken by the menu: never posted
	n := menus.Load()
	post(cocoatest.RightButton, true, x, y)
	if !waitMenus(n + 1) {
		t.Fatal("a right press in the composer opened no menu")
	}
	var lo, hi int
	ma.onUI(t, func() { lo, hi = ed.Selection() })
	t.Logf("the right press selected [%d, %d) (the word under it, as on macOS)", lo, hi)
	// a left click on "there"
	post(cocoatest.LeftButton, true, x+60, y)
	post(cocoatest.LeftButton, false, x+60, y)
	time.Sleep(time.Second)
	if got := menus.Load() - n; got != 1 {
		t.Errorf("a left click in the composer after its menu opened %d more menus, want none (the left press was a context click)", got-1)
	}
	ma.onUI(t, func() { lo, hi = ed.Selection() })
	if lo != hi {
		t.Errorf("the left click left [%d, %d) selected (%q), want the caret where it clicked", lo, hi, ma.selText(t, ed))
	}
}

// selText is the editor's selected text, read on the UI goroutine.
func (ma *macBoundsApp) selText(t *testing.T, ed *composer.Editor) (s string) {
	ma.onUI(t, func() { s = ed.SelectedText() })
	return s
}
