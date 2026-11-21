package ui

// The message hover toolbar where Slack draws it (hover-toolbar-pos;
// Karim, α.106 on Windows: "in thread via viewing via activty i hovered
// over this element and it was like way up"), measured from what the
// window drew: the inspect tool's outlines, Gio's own hit test over the
// frame's areas (the way Karim's capture was taken). The list's rows are
// read only to know where to point.
//
// Slack's MessageActions card (.c-message__actions) is position:
// absolute in its row's c-message_kit__actions wrapper: top -16px
// ("default"), -28px ("above", a grouped row), 4px ("inside", the thread
// pane's root: ThreadRoot's actionPosition), its right edge 17px in. It
// is inside the list's scroller (.c-virtual_list, overflow-y scroll), so
// it moves with its row and the list clips it: a row at the list's top
// edge has its card cut off, one scrolled above it has it out of view.
// Clack kept the card 4px under the list's top instead, so the row at the
// top of a jump to a message, and a thread root scrolled up, drew their
// toolbar away from the row, as far as the row's top was above the list.
//
// On fakeslack's thread pane audit fixtures (synthetic; nothing talks to
// Slack), in Karim's window size.

import (
	"context"
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// Slack's .c-message__actions tops (dp from the row's top) and right
// inset.
const (
	actsTopDefault = -16
	actsTopInside  = 4
	actsRight      = 17
)

// hoverToolbarEnv is the client on the thread pane audit fixtures with
// the inspect tool's shortcut on, in a 1918×1080 window, with newer
// messages in #th-audit after its threads (so a jump to a thread root
// isn't held at the list's end).
func hoverToolbarEnv(t *testing.T) *perfEnv {
	t.Helper()
	t.Setenv("CLACK_INSPECT", "1")
	e := threadsAuditEnv(t, false)
	// the plugin runtimes go with the test, and their patches with them
	// (slackc's registry is the process's: TabsShape's left
	// ClientPrimaryView and RenderedViewContents patched for the tests
	// after it, whose composer clicks then went nowhere)
	t.Cleanup(func() {
		if e.a.data != nil {
			e.a.data.stopTaut()
		}
	})
	if e.a.insp == nil {
		t.Fatal("CLACK_INSPECT=1 but the window has no inspect tool")
	}
	e.size = image.Pt(1918, 1080)
	users := e.fs.UserIDs()[1:4]
	for n := 0; n < 24; n++ {
		_, ev := e.fs.PostQuiet(fakeslack.ThreadsAuditChannel, users[n%len(users)], fmt.Sprintf("a synthetic newer message %d", n))
		e.fs.Push(ev)
	}
	e.settle(300 * time.Millisecond)
	return e
}

// inspectOn starts an inspect session.
func (e *perfEnv) inspectOn() {
	e.tb.Helper()
	e.hyperD()
	e.frame()
	if !e.a.insp.Active() {
		e.tb.Fatal("the shortcut started no inspect session")
	}
}

// drawnAt points at p and widens the inspect outline (↑) until its label
// starts with one of want: that outline is what the frame drew there
// (window px). The outline then narrows back (↓), so the next point
// starts from its innermost element.
func (e *perfEnv) drawnAt(p image.Point, want ...string) (string, image.Rectangle, bool) {
	e.move(f32.Pt(float32(p.X), float32(p.Y)))
	e.frame()
	up := 0
	defer func() {
		for ; up > 0; up-- {
			e.key(key.NameDownArrow, 0)
			e.frame()
		}
	}()
	for k := 0; k < 24; k++ {
		label, r, ok := e.a.insp.Hovered()
		if !ok {
			return "", image.Rectangle{}, false
		}
		for _, w := range want {
			if strings.HasPrefix(label, w) {
				return label, r, true
			}
		}
		e.key(key.NameUpArrow, 0)
		e.frame()
		up++
	}
	return "", image.Rectangle{}, false
}

// rowsLister is a message list as the test reads where its rows are.
type rowsLister interface {
	RowRects() []messages.RowRect
	ListRect() image.Rectangle
}

// rowPoint is a point in the part of row r the list shows, left of where
// a toolbar goes.
func rowPoint(r, list image.Rectangle) image.Point {
	top, bot := max(r.Min.Y, list.Min.Y), min(r.Max.Y, list.Max.Y)
	return image.Pt(r.Min.X+240, (top+bot)/2)
}

// checkToolbar hovers row ts of l as a pointer does, resting in the row
// and then going onto its toolbar's last button, More actions, where
// Slack draws it (the card's top at the row's top plus top, its right
// end 17 in from the row's, the button 4 inside both), and checks what
// the frame drew there against the row the frame drew (rowComp: the
// row's component): that button, in that place, in the MessageActions
// card at Slack's place, 40px tall.
func (e *perfEnv) checkToolbar(t *testing.T, what string, l rowsLister, ts, rowComp string, top int) {
	t.Helper()
	list := l.ListRect()
	var guide image.Rectangle
	for _, r := range l.RowRects() {
		if r.TS == ts {
			guide = r.R
		}
	}
	if guide.Empty() || !guide.Overlaps(list) {
		t.Fatalf("%s: row %s isn't on screen (list %v, rows %v)", what, ts, list, l.RowRects())
	}
	in := rowPoint(guide, list)
	_, row, ok := e.drawnAt(in, rowComp)
	if !ok {
		t.Fatalf("%s: no %s outline over row %s at %v", what, rowComp, ts, in)
	}
	cardTop, cardRight := row.Min.Y+top, row.Max.X-actsRight
	moreBtn := image.Rect(cardRight-4-32, cardTop+4, cardRight-4, cardTop+4+32)
	more := moreBtn.Min.Add(image.Pt(16, 16))
	if !more.In(list) || !more.In(row) {
		t.Fatalf("%s: Slack's More actions button of row %s (drawn %v) at %v isn't in the row in the list (%v)", what, ts, row, more, list)
	}
	// onto the toolbar from the row
	e.drawnAt(in, rowComp)
	label, btn, ok := e.drawnAt(more, "button.c-button-unstyled.c-icon_button")
	if !ok || btn != moreBtn {
		_, card, _ := e.drawnAt(more, "MessageActions")
		t.Errorf("%s: row %s drawn at %v: at %v the window drew %q %v (MessageActions there: %v), Slack's More actions button %v (card top %+d from the row's, right edge %d in)",
			what, ts, row, more, label, btn, card, moreBtn, top, actsRight)
		return
	}
	_, card, ok := e.drawnAt(more, "MessageActions")
	if !ok {
		t.Errorf("%s: row %s: no MessageActions outline around its More actions button", what, ts)
		return
	}
	if card.Min.Y-row.Min.Y != top || card.Max.X != cardRight || card.Dy() != 40 {
		t.Errorf("%s: row %s drawn at %v: toolbar card drawn at %v (top %+d from the row's, right edge %d in, %dpx tall), Slack's top %+d, right %d in, 40px",
			what, ts, row, card, card.Min.Y-row.Min.Y, row.Max.X-card.Max.X, card.Dy(), top, actsRight)
	}
}

// From Activity, a thread item opens the thread beside the inbox at the
// reply (OpenThreadAt, activity_panel.go openSecondary); the thread
// header's channel name opens the channel at the root (c/<id>/m/<root>,
// the channel jumped to the message and flashing it). Hovering the root
// and its neighbours, each toolbar is at Slack's place against its row,
// the row at the list's top too, its card cut off by the list's top edge
// (the jump puts the root 64dp under the list's top, BaseMessageList's
// ANCHOR_OFFSET, and the list is scrolled that far to bring it there).
func TestHoverToolbarJumpFromActivity(t *testing.T) {
	e := hoverToolbarEnv(t)
	e.jumpFromActivityToolbars(t, "jump from Activity")
}

// The same under a plugin of ChatTabs' shape (Slick's ChatTabs, which
// Karim runs: ClientPrimaryView in its layout, its tab bar handed to
// RenderedViewContents as the first child, absolute at the top, the view
// padded under it), so the channel is drawn inside the plugin's tree,
// measured and then drawn each frame.
func TestHoverToolbarJumpFromActivityChatTabs(t *testing.T) {
	t.Setenv("CLACK_HOME", t.TempDir())
	e := hoverToolbarEnv(t)
	e.until("Slick", 60*time.Second, func() bool { return e.a.Slick() != nil })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := e.a.Slick().Import(ctx, []byte(tabsShapePlugin)); err != nil {
		t.Fatal(err)
	}
	if err := e.a.Slick().SetEnabled(ctx, "TabsShape", true); err != nil {
		t.Fatal(err)
	}
	e.until("the primary view in the plugin's layout", 20*time.Second, func() bool {
		return slackc.Patched("ClientPrimaryView") && slackc.Patched("RenderedViewContents")
	})
	e.jumpFromActivityToolbars(t, "jump from Activity under ChatTabs' layout")
}

// jumpFromActivityToolbars is the route of TestHoverToolbarJumpFromActivity
// and its checks.
func (e *perfEnv) jumpFromActivityToolbars(t *testing.T, what string) {
	t.Helper()
	ch, root := fakeslack.ThreadsAuditChannel, fakeslack.ThreadsAuditLong
	e.a.Navigate("activity")
	e.settle(600 * time.Millisecond)
	e.a.OpenThreadAt(ch, root, fakeslack.ThreadsAuditReply(root, 30))
	var name image.Rectangle
	e.until("the thread header's channel link", 15*time.Second, func() bool {
		tv := e.threadView(ch, root)
		if tv == nil {
			return false
		}
		sub, r, ok := tv.HeaderName()
		name = r
		return ok && sub.Link && !r.Empty()
	})
	e.clickAt(name.Min.Add(name.Size().Div(2)), time.Second)
	want := Route("c/" + ch + "/m/" + root)
	e.until("the jump to the root", 5*time.Second, func() bool { return e.a.router.Current() == want })
	l := func() rowsLister {
		cl, _ := e.a.convoFor(ch, "").list.(channelList)
		return cl
	}
	e.until("the root on screen", 10*time.Second, func() bool {
		for _, r := range l().RowRects() {
			if r.TS == root {
				return true
			}
		}
		return false
	})
	e.settle(800 * time.Millisecond)
	e.pointerIn(l())
	rows := l().RowRects()
	list := l().ListRect()
	rootAt := func() int {
		for k, r := range rows {
			if r.TS == root {
				return k
			}
		}
		return -1
	}
	sc := e.scale
	if sc == 0 {
		sc = 1
	}
	anchor := int(64*sc + 0.5)
	i := rootAt()
	if i < 0 || rows[i].R.Min.Y-list.Min.Y != anchor {
		t.Fatalf("%s: the jump didn't put the root %dpx under the list's top (list %v, rows %v)", what, anchor, list, rows)
	}
	e.inspectOn()
	// where the jump put them: the root and the row under it (the row
	// above is cut off past its toolbar's button)
	for _, k := range []int{i, i + 1} {
		if k < len(rows) {
			e.checkToolbar(t, what, l(), rows[k].TS, "MessageWrapper", actsTopDefault)
		}
	}
	// the root's row at the list's top, its card cut off by the edge, and
	// the rows under it
	cl, _ := e.a.convoFor(ch, "").list.(channelList)
	cl.ScrollPx(anchor)
	e.pointerIn(l())
	rows, list = l().RowRects(), l().ListRect()
	if i = rootAt(); i < 0 || rows[i].R.Min.Y != list.Min.Y {
		t.Fatalf("%s: scrolled %dpx after the jump, the root isn't at the list's top (list %v, rows %v)", what, anchor, list, rows)
	}
	for _, k := range []int{i, i + 1, i + 2} {
		if k < len(rows) {
			e.checkToolbar(t, what+" (scrolled)", l(), rows[k].TS, "MessageWrapper", actsTopDefault)
		}
	}
}

// pointerIn brings the pointer into list l, so the window coordinates it
// reports are where it is (a list learns its window origin from the
// pointer: under a plugin's tree the panel's is not the list's).
func (e *perfEnv) pointerIn(l rowsLister) {
	r := l.ListRect()
	e.move(f32.Pt(float32(r.Min.X+r.Dx()/3), float32(r.Min.Y+r.Dy()/2)))
	e.frame()
	e.frame()
}

// tabsShapePlugin is ChatTabs' layout around the primary view
// (src/plugins/ChatTabs/index.tsx: ClientPrimaryView in a display:contents
// .slick-chat-tabs-layout, the bar handed to RenderedViewContents as its
// first child, absolute at the top, the view padded under it), with a
// fixed bar.
const tabsShapePlugin = `// slick-plugin {"id":"TabsShape"}
(globalThis.__slickPlugins ??= []).push((require) => {
var import_slick = require("$slick");
class P extends import_slick.SlickPlugin {
  static id = "TabsShape";
  static pluginName = "TabsShape";
  static description = "test: ChatTabs' layout around the primary view";
  static requiredAPIs = [];
  start() {
    this.api.setStyle(".tabs-shape-layout { display:contents; } .tabs-shape-layout .p-view_contents--primary:has(> .tabs-shape) { padding-top: 37px; } .tabs-shape { position:absolute; top:0; left:0; right:0; z-index:1; height:36px; background:#fff; border-bottom:1px solid #8885; }");
    const Bar = React.createContext(undefined);
    this.api.patchComponent("ClientPrimaryView", (Original) => (props) =>
      React.createElement("div", { className: "tabs-shape-layout" },
        React.createElement(Bar.Provider, { value: React.createElement("div", { className: "tabs-shape" }, "tabs") },
          React.createElement(Original, props))));
    this.api.patchComponent("RenderedViewContents", (Original) => (props) => {
      const bar = React.useContext(Bar);
      if (bar === undefined) return React.createElement(Original, props);
      return React.createElement(Original, props, bar, React.createElement(Bar.Provider, { value: undefined }, props.children));
    });
  }
}
return P;
});
`

// The thread pane opened from Activity at a reply, then read from its
// root: the root's toolbar sits 4px inside its top (Slack's "inside"),
// and scrolled 10px up it stays there, cut off; a reply's is 16px above
// it; at the root's top, a reply scrolled to the list's top has its card
// cut off as in the channel.
func TestHoverToolbarThreadPane(t *testing.T) {
	e := hoverToolbarEnv(t)
	ch, root := fakeslack.ThreadsAuditChannel, fakeslack.ThreadsAuditLong
	e.a.Navigate("activity")
	e.settle(600 * time.Millisecond)
	e.a.OpenThreadAt(ch, root, fakeslack.ThreadsAuditReply(root, 30))
	l := func() rowsLister {
		if tv := e.threadView(ch, root); tv != nil {
			return tv
		}
		return nil
	}
	e.until("the thread's replies", 15*time.Second, func() bool {
		tl := l()
		return tl != nil && len(tl.RowRects()) > 3
	})
	e.settle(600 * time.Millisecond)
	e.pointerIn(l())
	list := l().ListRect()
	p := image.Pt(list.Min.X+list.Dx()/2, list.Min.Y+list.Dy()/2)
	// to the root: the top of the thread
	e.scroll(p, -200000)
	e.settle(400 * time.Millisecond)
	rootAt := func() image.Rectangle {
		for _, r := range l().RowRects() {
			if r.TS == root {
				return r.R
			}
		}
		return image.Rectangle{}
	}
	if r := rootAt(); r.Min.Y != list.Min.Y {
		t.Fatalf("the thread isn't at its root (root %v, list %v)", r, list)
	}
	e.inspectOn()
	e.checkToolbar(t, "thread pane, at the root", l(), root, "ThreadRootGeneric", actsTopInside)
	rows := l().RowRects()
	if len(rows) > 2 {
		// a reply in the thread pane is ThreadReply → ThreadReplyGeneric, as
		// in Slack's thread pane (modern.vendor), with no MessageWrapper:
		// that's the channel list's alone (plugin-render-3)
		e.checkToolbar(t, "thread pane, a reply", l(), rows[2].TS, "ThreadReplyGeneric", actsTopDefault)
	}
	// 10px down: the root's top above the list's
	e.a.insp.Toggle()
	e.frame()
	e.scroll(p, 10)
	e.settle(300 * time.Millisecond)
	if r := rootAt(); r.Min.Y != list.Min.Y-10 {
		t.Fatalf("10px down: the root at %v, want its top 10px above the list's (%v)", r, list)
	}
	e.inspectOn()
	e.checkToolbar(t, "thread pane, the root scrolled 10px up", l(), root, "ThreadRootGeneric", actsTopInside)
	// a reply at the list's top
	e.a.insp.Toggle()
	e.frame()
	rows = l().RowRects()
	var reply messages.RowRect
	for _, r := range rows {
		if r.TS != root && r.R.Min.Y > list.Min.Y+100 {
			reply = r
			break
		}
	}
	if reply.TS == "" {
		t.Fatalf("no reply under the root (rows %v)", rows)
	}
	e.scroll(p, float32(reply.R.Min.Y-list.Min.Y))
	e.settle(300 * time.Millisecond)
	for _, r := range l().RowRects() {
		if r.TS == reply.TS && r.R.Min.Y != list.Min.Y {
			t.Fatalf("the reply isn't at the list's top after the scroll (%v, list %v)", r.R, list)
		}
	}
	e.inspectOn()
	e.checkToolbar(t, "thread pane, a reply at the list's top", l(), reply.TS, "ThreadReplyGeneric", actsTopDefault)
}
