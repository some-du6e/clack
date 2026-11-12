package popups

import (
	"image"
	"sync"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// The quick picker's Reactions tab (HuddleQuickEmojiPicker, boot-shared
// Vw0B) is the bare EmojiPicker with hideTabs: no Emoji | GIFs row (that's
// EmojiAndGIFPickerPopover's), no group tabs, the search box 7px from the
// top and the list under it, the footer at the bottom of the 200px
// (compact) panel. Down then Enter picks the first emoji and closes it,
// Escape closes it, and a still picker asks for no frames.
func TestEmbeddedEmojiPickerHideTabs(t *testing.T) {
	h, _ := pfEnv(t)
	var picked []string
	p := NewEmbeddedEmojiPicker(h, func(name string) { picked = append(picked, name) })
	var (
		r     input.Router
		ops   op.Ops
		mu    sync.Mutex
		drawn = map[string]bool{}
	)
	const W, H = 360, 200
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(W, H)), Source: r.Source(), Now: time.Now()}
		p.LayoutEmbedded(gtx)
		r.Frame(&ops)
	}
	theme.DrawHook = func(s string) { mu.Lock(); drawn[s] = true; mu.Unlock() }
	frame()
	frame()
	theme.DrawHook = nil
	for _, s := range []string{"Emoji", "GIFs"} {
		if drawn[s] {
			t.Errorf("the embedded picker drew the %q tab (hideTabs, no EmojiAndGIFPickerPopover)", s)
		}
	}
	if !drawn["Search all emoji"] {
		t.Errorf("no search placeholder; drew %v", drawn)
	}
	// the list: from 7 + 36 + 7 = 50 to the 60px footer
	if want := H - 50 - epFootH; p.viewH != want {
		t.Errorf("list %d px tall, want %d (top 50, footer %d)", p.viewH, want, epFootH)
	}
	// still: no immediate redraw (the tab row's catch-up invalidate is
	// off; the search box's caret blink is a later deadline)
	if at, ok := r.WakeupTime(); ok && at.Before(time.Now().Add(100*time.Millisecond)) {
		t.Error("a still embedded picker asked for the next frame at once")
	}
	press := func(n key.Name) {
		r.Queue(key.Event{Name: n, State: key.Press}, key.Event{Name: n, State: key.Release})
		frame()
		frame()
	}
	press(key.NameDownArrow)
	if p.sel.row < 0 {
		t.Fatalf("Down selected nothing: %+v", p.sel)
	}
	press(key.NameReturn)
	if len(picked) != 1 || picked[0] == "" || !p.Dismissed() {
		t.Fatalf("Enter: picked %q, dismissed %v", picked, p.Dismissed())
	}

	q := NewEmbeddedEmojiPicker(h, func(string) { t.Error("Escape picked an emoji") })
	p = q
	frame()
	frame()
	press(key.NameEscape)
	if !q.Dismissed() {
		t.Error("Escape didn't close the embedded picker")
	}
}
