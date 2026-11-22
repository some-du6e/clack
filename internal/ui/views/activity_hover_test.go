package views

import (
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Activity's hover actions as Slack's ActivityRowContentActions: Mark as
// read on an unread item (Mark as unread on a read one, none on a read
// reaction), Clear, More actions; each a MessageActionButton with its
// label for a tooltip. More actions opens the item's menu under the
// button (bottom-right, 6px below, 300px wide), and the bar stays up while
// the menu is open, whatever the pointer does (setActionsVisible), until
// Escape or a click outside closes it; then it goes with the pointer off
// the row.
func TestActivityHoverBarLikeSlack(t *testing.T) {
	v, h, e, _ := vaFakeActivity(t, false)
	// the window's pointer, as the ui root gives it (the More menu's
	// anchor is the button under it)
	var last image.Point
	old := theme.WindowPointer
	theme.WindowPointer = func() (image.Point, bool) { return last, true }
	t.Cleanup(func() { theme.WindowPointer = old })
	// room under every row for the 300px menu (keepInBounds would move it)
	e.size.Y = 2400
	e.frame()
	e.frame()
	for _, c := range []struct {
		key  string
		want []actBtn
	}{
		{"fa-reaction", []actBtn{actBtnMarkRead, actBtnClear, actBtnMore}},
		{"fa-dm-reacted", []actBtn{actBtnMarkUnread, actBtnClear, actBtnMore}},
	} {
		r := v.rowFor(c.key)
		if got := v.hoverButtons(r.it, v.isUnread(r.it, c.key)); !slicesEqual(got, c.want) {
			t.Errorf("%s: buttons %v, want %v", c.key, got, c.want)
		}
	}
	const rowKey = "fa-dm-reacted"
	r := v.rowFor(rowKey)
	at := func(c interface{}) (image.Rectangle, bool) {
		full, _, ok := e.r.AreaBounds(c)
		return full, ok
	}
	move := func(p image.Point) {
		last = p
		e.r.Queue(asEvents([]pointer.Event{{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X)+.5, float32(p.Y)+.5)}})...)
		e.frame()
		e.frame()
	}
	row, ok := at(&r.click)
	if !ok {
		t.Fatal("the row isn't drawn")
	}
	move(image.Pt(row.Min.X+40, row.Min.Y+row.Dy()/2))
	more, ok := at(&r.btns[actBtnMore])
	if !ok {
		t.Fatal("the hovered row has no More actions button")
	}
	if _, ok := at(&r.btns[actBtnMarkUnread]); !ok {
		t.Error("the hovered read row has no Mark as unread button")
	}
	// the button's tooltip is its label
	if _, label := actBtnIcon(actBtnMore); label != "More actions" {
		t.Errorf("More actions' label %q", label)
	}
	open := func() *popups.Menu {
		n := len(h.open())
		c := &rxCase{e: e}
		last = image.Pt(more.Min.X+more.Dx()/2, more.Min.Y+more.Dy()/2)
		c.click(f32.Pt(float32(last.X)+.5, float32(last.Y)+.5))
		os := h.open()
		for i := len(os) - 1; i >= n; i-- {
			if m, ok := os[i].(*popups.Menu); ok {
				return m
			}
		}
		t.Fatal("More actions opened no menu")
		return nil
	}
	m := open()
	e.frame()
	mr := m.Rect()
	if mr.Dx() != 300 {
		t.Errorf("the menu is %dpx wide, want 300", mr.Dx())
	}
	if mr.Min.Y < more.Max.Y+6 || mr.Min.Y > more.Max.Y+8 {
		t.Errorf("the menu's top at %d, want 6px under the button (bottom %d)", mr.Min.Y, more.Max.Y)
	}
	if mr.Max.X > more.Max.X+1 || mr.Max.X < more.Max.X-1 {
		t.Errorf("the menu's right edge at %d, want the button's (%d)", mr.Max.X, more.Max.X)
	}
	// the pointer leaves the row for the menu: the bar stays
	move(image.Pt(mr.Min.X+20, mr.Min.Y+20))
	if _, ok := at(&r.btns[actBtnMore]); !ok {
		t.Error("the bar went while its menu is open")
	}
	// Escape closes it; with the pointer off the row the bar goes
	e.r.Queue(key.Event{Name: key.NameEscape, State: key.Press}, key.Event{Name: key.NameEscape, State: key.Release})
	e.frame()
	move(image.Pt(5, e.size.Y-5))
	if !m.Dismissed() {
		t.Fatal("Escape didn't close the menu")
	}
	if v.selOK && v.selKey == rowKey {
		t.Fatal("the row got selected: the bar's own rule keeps it")
	}
	if _, ok := at(&r.btns[actBtnMore]); ok {
		t.Error("the bar stayed after the menu closed with the pointer off the row")
	}
	// a click outside closes it too
	move(image.Pt(row.Min.X+40, row.Min.Y+row.Dy()/2))
	if more, ok = at(&r.btns[actBtnMore]); !ok {
		t.Fatal("hovered again, the row has no bar")
	}
	m = open()
	e.frame()
	c := &rxCase{e: e}
	last = image.Pt(5, e.size.Y-5)
	c.click(f32.Pt(5, float32(e.size.Y-5)))
	move(image.Pt(5, e.size.Y-5))
	if !m.Dismissed() {
		t.Fatal("a click outside didn't close the menu")
	}
	if _, ok := at(&r.btns[actBtnMore]); ok {
		t.Error("the bar stayed after an outside click closed its menu")
	}
}

func slicesEqual(a, b []actBtn) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
