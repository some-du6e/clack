package ui

import (
	"image"
	"runtime"
	"slices"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The top bar's search box is Slack's BaseSearch: "Search {team}" until a
// search ran, then "Search: {query}" with a clear button that empties it
// (the icon hides on Windows and Linux meanwhile). A primary press opens
// the search at once (onMouseDown), its release opens nothing more;
// Ctrl+press opens nothing; Enter/Space (the Clickable) still opens it.
func TestSearchTopBarBox(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	pe := startEnv(t, fs, t.TempDir())
	pe.waitReady()
	pe.openChannel(fs.ChannelIDs()[1])
	pe.settle(300 * time.Millisecond)
	e := &mfEnv{t: t, a: pe.a, rt: &pe.r, frame: pe.frame, size: pe.size}
	scan := func() []string {
		stop := theme.StartTofuScan()
		e.frame()
		return stop().Texts
	}
	placeholder := "Search " + e.a.teamName()
	if x := scan(); !slices.Contains(x, placeholder) {
		t.Fatalf("no %q in the top bar: %q", placeholder, x)
	}

	e.a.Navigate("search?q=zephyr")
	e.frames(3)
	if x := scan(); !slices.Contains(x, "Search: zephyr") {
		t.Fatalf("with a query the box reads %q", x)
	}
	if got := e.a.searchBoxIcon(); got != (runtime.GOOS == "darwin") {
		t.Errorf("search icon shown %v with a query on %s", got, runtime.GOOS)
	}

	// a press opens the search before the release; the release adds nothing
	box := e.a.clicks.get("nav.search")
	p, ok := e.hoverFind(box, image.Rect(0, 0, e.size.X/2, theme.TopNavH))
	if !ok {
		t.Fatal("no search box under the pointer")
	}
	n := len(e.a.overlays)
	e.ptr(pointer.Press, p)
	e.frames(2)
	if len(e.a.overlays) <= n {
		t.Fatal("a press on the search box didn't open the search")
	}
	m := len(e.a.overlays)
	e.ptr(pointer.Release, p)
	e.frames(3)
	if len(e.a.overlays) != m {
		t.Errorf("the release opened another overlay (%d → %d)", m, len(e.a.overlays))
	}
	e.dismiss()

	// Ctrl+press opens nothing
	e.ptr(pointer.Move, p)
	e.frame()
	n = len(e.a.overlays)
	e.clock += 20 * time.Millisecond
	e.rt.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y)),
		Buttons: pointer.ButtonPrimary, Modifiers: key.ModCtrl, Time: e.clock})
	e.frames(2)
	e.ptr(pointer.Release, p)
	e.frames(3)
	if len(e.a.overlays) != n {
		t.Errorf("Ctrl+press opened the search (%d → %d overlays)", n, len(e.a.overlays))
	}
	e.dismiss()

	// the clear button empties the box without opening the search
	clear := e.a.clicks.get("nav.search.clear")
	cp, ok := e.hoverFind(clear, image.Rect(0, 0, e.size.X, theme.TopNavH))
	if !ok {
		t.Fatal("no clear button with a query")
	}
	n = len(e.a.overlays)
	e.ptr(pointer.Press, cp)
	e.ptr(pointer.Release, cp)
	e.frames(3)
	if e.a.searchQuery != "" {
		t.Errorf("the clear button left %q", e.a.searchQuery)
	}
	if len(e.a.overlays) != n {
		t.Error("the clear button opened the search")
	}
	if x := scan(); !slices.Contains(x, placeholder) || slices.Contains(x, "Search: zephyr") {
		t.Errorf("after clearing the box reads %q", x)
	}
	e.dismiss()

	// a keyboard click (no press) still opens it
	box.Click()
	e.frames(2)
	if len(e.a.overlays) <= n {
		t.Error("Enter/Space on the box didn't open the search")
	}
	e.dismiss()
}
