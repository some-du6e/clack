package views

import (
	"encoding/json"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
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
	if mr.Min.Y < more.Max.Y+5 || mr.Min.Y > more.Max.Y+7 { // 6, the press's sub-pixel aside
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

// Activity's rows preview a message as Slack's SimpleRichTextPreview
// does: a rich_text link is a plain hyperlink, a truncated one (a pasted
// link's slug) too, never the message's RichTextSlug chip with its link
// icon (Karim's 2026-11-22 "fix": a dense row's link chip after its type
// icon), in the dense and the detailed layout.
func TestActivityPreviewLinksArePlain(t *testing.T) {
	for _, dense := range []bool{false, true} {
		const ts = "1762700500.000100" // fa-reaction's (vaFakeActivity's store holds the mentions' already)
		ch := fakeslack.ViewsChannel
		blocks := json.RawMessage(`[{"type":"rich_text","block_id":"b1","elements":[{"type":"rich_text_section","elements":[` +
			`{"type":"link","url":"` + rhLink + `","text":"example.com/doc","truncated":true},{"type":"text","text":" is the plan"}]}]}]`)
		v, _, e, _ := vaFakeActivityWith(t, false, func(fs *fakeslack.Server) {
			fs.SetActivityMessage(slack.Message{Type: "message", Channel: ch, TS: ts, User: fakeslack.SelfID,
				Text: "<" + rhLink + "|example.com/doc> is the plan", Blocks: blocks,
				Reactions: []slack.Reaction{{Name: "tada", Count: 2, Users: []string{"U0FAKE00001", "U0FAKE00002"}}}})
		})
		v.dense, v.rows, v.dirty = dense, map[string]*actRow{}, true
		e.frame()
		e.frame()
		var ent *richEntry
		e.until(t, "the row's preview", func() bool {
			for k, x := range v.rich {
				if strings.HasSuffix(k, ch+"/"+ts) && len(x.v.LinkRects()) > 0 {
					ent = x
					return true
				}
			}
			return false
		})
		doc := ent.v.Doc()
		links := 0
		for _, b := range doc.Blocks {
			for _, sp := range b.Spans {
				if sp.Kind == richtext.Link {
					links++
					if sp.Truncated {
						t.Errorf("dense %v: the preview draws the link as its slug chip (truncated)", dense)
					}
					if sp.Text != "example.com/doc" || sp.URL != rhLink {
						t.Errorf("dense %v: the link reads %q -> %q", dense, sp.Text, sp.URL)
					}
				}
			}
		}
		if links != 1 {
			t.Errorf("dense %v: %d links in the preview, want 1", dense, links)
		}
		if lr := ent.v.LinkRects(); lr[0].URL != rhLink {
			t.Errorf("dense %v: the drawn link goes to %q", dense, lr[0].URL)
		}
	}
}

// Every Activity row is cursor: pointer, so a mention row's text (no
// subtext before it) shows the hand as a reply's does, in the dense and
// the detailed layout.
func TestActivityEveryRowHasTheHand(t *testing.T) {
	for _, dense := range []bool{false, true} {
		v, _, e, _ := vaFakeActivity(t, false)
		e.size.Y = 2400
		v.dense, v.rows, v.dirty = dense, map[string]*actRow{}, true
		e.frame()
		e.frame()
		ch, ts := fakeslack.ViewsChannel, "1762700100.000100" // fa-at-user's
		var ent *richEntry
		e.until(t, "the mention's preview", func() bool {
			for k, x := range v.rich {
				if strings.HasSuffix(k, ch+"/"+ts) {
					ent = x
					return true
				}
			}
			return false
		})
		e.frame()
		full, _, ok := e.r.AreaBounds(ent.v)
		if !ok {
			t.Fatalf("dense %v: the mention's preview has no area", dense)
		}
		q := full.Min.Add(textPoint(t, ent.v))
		if cur := pointerAt(e, f32.Pt(float32(q.X)+.5, float32(q.Y)+.5)); cur != pointer.CursorPointer {
			t.Errorf("dense %v: the cursor over a mention row's text is %v, want the row's hand", dense, cur)
		}
	}
}

// A row click whose press and release came in one frame is decided in the
// next (clickLater); one whose next frame never comes in time (the view
// not drawn, the row not laid out) is dropped, not opened later.
func TestActivityLateClickExpires(t *testing.T) {
	v, _, e, _ := vaFakeActivity(t, false)
	e.size.Y = 2400
	e.frame()
	const rowKey = "fa-dm-reacted"
	r := v.rowFor(rowKey)
	row, _, ok := e.r.AreaBounds(&r.click)
	if !ok {
		t.Fatal("the row isn't drawn")
	}
	p := f32.Pt(float32(row.Min.X+60), float32(row.Min.Y+row.Dy()/2))
	click := func() {
		e.r.Queue(asEvents([]pointer.Event{{Kind: pointer.Move, Source: pointer.Mouse, Position: p},
			{Kind: pointer.Press, Source: pointer.Mouse, Position: p, Buttons: pointer.ButtonPrimary},
			{Kind: pointer.Release, Source: pointer.Mouse, Position: p}})...)
	}
	// the view isn't drawn after the click's frame: its click goes
	draw := e.draw
	click()
	e.frame()
	if !r.clickLater {
		t.Fatal("a press and release in one frame weren't deferred: the test checks nothing")
	}
	e.draw = func(gtx layout.Context) {}
	e.frame()
	time.Sleep(actClickLaterMax + 100*time.Millisecond)
	e.draw = draw
	e.frame()
	e.frame()
	if v.selOK && v.selKey == rowKey {
		t.Error("a deferred click opened the row long after it came")
	}
	// decided in the next frame, it opens the row
	click()
	e.frame()
	e.frame()
	if !v.selOK || v.selKey != rowKey {
		t.Errorf("a click decided in its next frame didn't open the row (open %v %q)", v.selOK, v.selKey)
	}
}
