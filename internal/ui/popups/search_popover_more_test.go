package popups

// "Show fewer" clicked in the quick switcher's blank slate (review of
// thread-header-hover-4, a crash): activate on a "Show N more" / "Show
// fewer" row rebuilds the rows without closing, and the click loop went
// on reading p.rows[i] past the new end (index out of range: Show fewer
// takes rows away with Recent places below it). Clicked through Layout,
// as a hand does: the press and the release in frames of their own.

import (
	"image"
	"slices"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
)

func TestSearchPopoverShowFewerClick(t *testing.T) {
	e := srNewEnv(t, false)
	p := NewQuickSwitcher(e.h)
	d := &srDrv{size: image.Pt(1280, 800), w: p}
	d.until(t, "the snapshot", func() bool { return p.snapOK })
	var ids []string
	for id := range p.byConv {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	if len(ids) < 13 {
		t.Fatalf("fixture: %d conversations", len(ids))
	}
	p.unreads = ids[:13]
	// Recent places below the unreads: pages and channels that aren't
	// among them (an unread is listed there, not here)
	RecordRecentPlace("view:later", "To-dos", "todos")
	RecordRecentPlace("view:saved", "Saved", "saved")
	for _, id := range ids[13:] {
		if e.h.a.ConvRef(id).Name != "" {
			RecordRecentPlace(id, "", "")
		}
	}
	p.rebuild(time.Now(), true)
	for i := 0; i < 3; i++ {
		d.frame()
	}
	clk := time.Second
	// clickRow hovers the popover until the row titled title reads
	// hovered, then presses and releases there in frames of their own
	clickRow := func(title string) {
		t.Helper()
		k := -1
		for i := range p.rows {
			if p.rows[i].kind == srRMore && p.rows[i].title == title {
				k = i
			}
		}
		if k < 0 {
			t.Fatalf("no %q row: %q", title, srTitles(p.rows, srRMore))
		}
		r := p.Rect()
		var at f32.Point
		found := false
		for y := r.Min.Y; y < r.Max.Y && !found; y += 6 {
			x := r.Min.X + 40
			clk += time.Millisecond
			q := f32.Pt(float32(x), float32(y))
			d.r.Queue(pointer.Event{Kind: pointer.Move, Position: q, Source: pointer.Mouse, Time: clk})
			d.frame()
			if k < len(p.rows) && spClick(p, k).Hovered() {
				at, found = q, true
			}
		}
		if !found {
			t.Fatalf("no point over the %q row in %v", title, r)
		}
		d.r.Queue(pointer.Event{Kind: pointer.Press, Position: at, Buttons: pointer.ButtonPrimary, Source: pointer.Mouse, Time: clk + 10*time.Millisecond})
		d.frame()
		d.r.Queue(pointer.Event{Kind: pointer.Release, Position: at, Source: pointer.Mouse, Time: clk + 90*time.Millisecond})
		d.frame()
		d.frame()
		clk += time.Second
	}
	clickRow("Show 3 more")
	if got := srTitles(p.rows, srRMore); !slices.Equal(got, []string{"Show fewer"}) {
		t.Fatalf("after Show 3 more: %q", got)
	}
	// the crash needs rows below Show fewer (Recent places): the loop
	// went on past the shorter list's end
	below := 0
	for i := range p.rows {
		if p.rows[i].kind == srRMore {
			below = len(p.rows) - 1 - i
		}
	}
	if below < 3 {
		t.Fatalf("fixture: %d rows below Show fewer, want Recent places there (%q)", below, srTitles(p.rows, srRHeader))
	}
	clickRow("Show fewer")
	if got := srTitles(p.rows, srRMore); !slices.Equal(got, []string{"Show 3 more"}) {
		t.Errorf("after Show fewer: %q, want the ten again with Show 3 more", got)
	}
}
