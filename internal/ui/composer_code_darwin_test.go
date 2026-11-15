//go:build darwin && cgo

package ui

import (
	"strings"
	"testing"
	"time"

	"gioui.org/app/cocoatest"

	"github.com/some-du6e/clack/internal/ui/composer"
)

// TestMacComposerFormatKeys is the composer's format shortcuts on a
// selection in App.Run's own window on a real macOS desktop
// (CLACK_MACOS_WINDOW_TESTS=1, ci's macos job), signed in to
// internal/fakeslack, with its menu bar: the keys go through NSApp
// sendEvent:, the menu's key equivalents (Edit's Copy is ⌘C, Cut ⌘X)
// and Gio's view (cocoatest.PostKey). Slack's SlackKeyboard binds code to
// {key:"c", shiftKey, shortKey}, strike to "x" and bold to "B"
// (applyFormattingFromShortcut → applyFormattingToSelection): each
// toggles its format on the selection, a second press takes it off.
// Karim (α.85): ⇧⌘C on a selection didn't make it code.
func TestMacComposerFormatKeys(t *testing.T) {
	if !macWindowTests {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	t.Setenv("CLACK_INSPECT", "")
	ma := runMacBoundsApp(t, nil)
	defer ma.quit(t, true)
	onMain(func() {
		cocoatest.OrderFront(ma.view)
		cocoatest.TraceKeys(ma.view)
	})
	ma.until(t, "the key window", func() bool { return ma.state().Key })

	var ed *composer.Editor
	ma.until(t, "the channel composer", func() bool {
		ma.onUI(t, func() {
			ch, ok := strings.CutPrefix(string(ma.a.router.Current()), "c/")
			if !ok {
				return
			}
			if cv := ma.a.convos[ch+"/"]; cv != nil && cv.comp != nil && !cv.compRect.Empty() {
				if cb, ok := cv.comp.(composerBox); ok {
					ed, _ = cb.FocusTag().(*composer.Editor)
					cb.Focus()
				}
			}
		})
		return ed != nil
	})
	const text = "make this formatted"
	const lo, hi = 5, 9 // "this"
	ma.onUI(t, func() {
		d := composer.NewDoc()
		d.Insert(0, text, composer.Attr{})
		ed.SetDoc(d)
		ed.SetSelection(lo, hi)
	})
	time.Sleep(time.Second) // the focus request's frame
	styled := func(s composer.Style) (all, none bool) {
		ma.onUI(t, func() {
			d := ed.Doc()
			all, none = d.HasStyle(lo, hi, s), true
			for i := 0; i < d.Len(); i++ {
				if d.Attrs[i].Style&s != 0 {
					none = false
				}
			}
		})
		return all, none
	}
	const cmd, shift = cocoatest.Cmd, cocoatest.Shift
	for _, c := range []struct {
		name  string
		code  int
		mods  int
		style composer.Style
	}{
		{"⇧⌘C", cocoatest.KeyC, cmd | shift, composer.Code},
		{"⇧⌘X", cocoatest.KeyX, cmd | shift, composer.Strike},
		{"⌘B", cocoatest.KeyB, cmd, composer.Bold},
	} {
		for _, want := range []bool{true, false} {
			var chars, ign string
			onMain(func() { chars, ign = cocoatest.PostKey(ma.view, c.code, c.mods) })
			deadline := time.Now().Add(5 * time.Second)
			for {
				all, none := styled(c.style)
				if want && all || !want && none {
					break
				}
				if time.Now().After(deadline) {
					var trace string
					onMain(func() { trace = cocoatest.KeyTrace() })
					var sa, sc int
					ma.onUI(t, func() { sa, sc = ed.Selection() })
					t.Fatalf("%s (characters %+q, ignoring %+q) with %q [%d,%d) selected (now %d,%d): format on %v, want %v; the view got:\n%s",
						c.name, chars, ign, text, lo, hi, sa, sc, all, want, trace)
				}
				time.Sleep(20 * time.Millisecond)
			}
			var trace string
			onMain(func() { trace = cocoatest.KeyTrace() })
			t.Logf("%s → on %v; the view got:\n%s", c.name, want, trace)
		}
	}
}
