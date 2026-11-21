package ui

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/shortcut"
)

// Which openings of a thread focus its reply box: Slack's pending
// threadMetadata.focus[windowId].focusInput (JhVT, build 35b42de8b870,
// thread_focus_input.go). The box focuses when the pane's input row
// mounts while the key is the thread's. The context menu's Reply in
// thread (T or a click) and the hover toolbar's Reply set it (the comment
// thunk 0yz+), as does the reply bar's openThread on a click without the
// split modifier; a permalink, a notification or Activity never do. The
// channel list's menu keys a broadcast reply by its own ts, so its
// parent's box stays unfocused; the toolbar keys it by thread_ts. A thread
// already open remounts nothing and doesn't refocus. Karim, 2026-11-21:
// "right clicking a message and then doing t brings up the thread side
// panel ... but does in slack focus the thread textbox?" ... "yes it does".
// Synthetic: fakeslack's thread header fixtures (#th-launch-notes, a root
// with two replies).

type rfEnv struct {
	*perfEnv
	t        *testing.T
	ch, root string
	bc       string        // a broadcast reply to root, also in the channel ("" none)
	clk      time.Duration // pointer event times
	inSplit  bool          // the rows are the split view's channel's (cv)
}

// newRFEnv opens #th-launch-notes with its root (and the broadcast) on
// screen and the channel's composer focused, as focusAfterNavigate
// leaves it. inspect installs the inspect tool (CLACK_INSPECT), which
// finds the toolbar's Reply button and the reply bar where they are drawn.
func newRFEnv(t *testing.T, inspect, broadcast bool) *rfEnv {
	t.Helper()
	if inspect {
		t.Setenv("CLACK_INSPECT", "1")
	}
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	x := &rfEnv{t: t, ch: fakeslack.ThreadPublicID, root: fakeslack.ThreadRootTS, clk: time.Second}
	if broadcast {
		x.bc = fs.PostBroadcastQuiet(x.ch, x.root, fs.UserIDs()[1], "a synthetic reply also sent to the channel")
	}
	x.perfEnv = startEnv(t, fs, t.TempDir())
	t.Cleanup(func() {
		if x.a.data != nil {
			x.a.data.stopTaut()
		}
	})
	if inspect && x.a.insp == nil {
		t.Fatal("CLACK_INSPECT=1 but the window has no inspect tool")
	}
	x.waitReady()
	x.openChannel(x.ch)
	x.until("history", 20*time.Second, func() bool { return x.loadedMessages(x.ch) > 0 })
	x.until("the root on screen", 15*time.Second, func() bool { _, ok := x.row(x.root); return ok })
	if broadcast {
		x.until("the broadcast on screen", 15*time.Second, func() bool { _, ok := x.row(x.bc); return ok })
	}
	x.settle(300 * time.Millisecond)
	x.focusComposer(x.ch, "")
	if !x.focused(x.ch, "") {
		t.Fatal("the channel's composer didn't take the focus")
	}
	return x
}

func (x *rfEnv) tick() time.Duration {
	x.clk += time.Second
	return x.clk
}

// row is message ts's row in the channel's list, when the list shows it.
// cv is the channel's list: the main view's, or with inSplit the split
// view's (its conversations are its pane's, split.go inPane).
func (x *rfEnv) cv() (cv *messages.ChannelView) {
	if !x.inSplit {
		return x.channelView(x.ch)
	}
	x.a.inPane(paneSplit, func() { cv = x.channelView(x.ch) })
	return cv
}

func (x *rfEnv) row(ts string) (image.Rectangle, bool) {
	cv := x.cv()
	if cv == nil {
		return image.Rectangle{}, false
	}
	list := cv.ListRect()
	for _, r := range cv.RowRects() {
		if r.TS == ts && r.R.Overlaps(list) {
			return r.R, true
		}
	}
	return image.Rectangle{}, false
}

func (x *rfEnv) box(ch, ts string) composerBox {
	x.t.Helper()
	c := x.a.convos[ch+"/"+ts]
	if c == nil {
		x.t.Fatalf("no conversation %s/%s", ch, ts)
	}
	cb, ok := c.comp.(composerBox)
	if !ok {
		x.t.Fatalf("%s/%s's composer is a %T", ch, ts, c.comp)
	}
	return cb
}

func (x *rfEnv) focused(ch, ts string) bool {
	return x.r.Source().Focused(x.box(ch, ts).FocusTag())
}

// threadFocused is whether the root's thread box has the key focus (false
// with no thread conversation yet).
func (x *rfEnv) threadFocused() bool {
	c := x.a.convos[x.ch+"/"+x.root]
	if c == nil || c.comp == nil {
		return false
	}
	cb, ok := c.comp.(composerBox)
	return ok && x.r.Source().Focused(cb.FocusTag())
}

// waitThreadBox runs frames until the side pane shows the root's thread
// with its reply box (the thread loaded: Slack's input row).
func (x *rfEnv) waitThreadBox(what string) {
	x.t.Helper()
	x.until(what, 15*time.Second, func() bool {
		s := x.a.secondary
		if s.kind != secThread || s.hidden || s.channel != x.ch || s.ts != x.root {
			return false
		}
		tv := x.threadView(x.ch, x.root)
		return tv != nil && !tv.HidesComposer() && !replyBoxRect(x.a.convos[x.ch+"/"+x.root]).Empty()
	})
}

// openMenu right-clicks row ts away from its text and under the hover
// toolbar (Slack opens its own menu only off text, Ay9+) and waits for
// the context menu with its thread row.
func (x *rfEnv) openMenu(ts string) {
	x.t.Helper()
	r, ok := x.row(ts)
	if !ok {
		x.t.Fatalf("row %s isn't on screen", ts)
	}
	x.rightClickAt(image.Pt(r.Max.X-100, (r.Min.Y+r.Max.Y)/2), x.tick())
	x.until("the context menu with Reply in thread", 8*time.Second, func() bool {
		m := x.topMenu()
		return m != nil && menuIndex(m.Items(), "comment") >= 0
	})
	x.settle(100 * time.Millisecond)
}

// wantThreadFocus checks that the thread's box has the focus and the
// channel's doesn't, and that typing goes into the thread's box.
func (x *rfEnv) wantThreadFocus(what string) {
	x.t.Helper()
	if !x.threadFocused() {
		x.t.Errorf("%s: the thread's reply box isn't focused (Slack's focusInput focuses it as it mounts)", what)
	}
	if x.focused(x.ch, "") {
		x.t.Errorf("%s: the channel's composer still has the focus", what)
	}
	// drafts are the process's (composer.drafts): compare with what the
	// boxes held, and take the text out again after
	const s = "rf-typed-text"
	thr, chn := x.box(x.ch, x.root).DocText(), x.box(x.ch, "").DocText()
	x.typeText(s)
	x.frame()
	x.frame()
	if got := x.box(x.ch, x.root).DocText(); got == thr || !strings.Contains(got, s) {
		x.t.Errorf("%s: typing left the thread's box %q (was %q), want it to take %q", what, got, thr, s)
	}
	if got := x.box(x.ch, "").DocText(); got != chn {
		x.t.Errorf("%s: typing went into the channel's composer (%q, was %q)", what, got, chn)
	}
	for range len(s) {
		x.key(key.NameDeleteBackward, 0)
	}
	x.frame()
	x.frame()
}

// wantNoThreadFocus checks the thread's box was left unfocused.
func (x *rfEnv) wantNoThreadFocus(what string) {
	x.t.Helper()
	if x.threadFocused() {
		x.t.Errorf("%s: the thread's reply box took the focus; Slack sets no focusInput for this one", what)
	}
}

// drawnScan tries the points in turn and returns the first where the
// inspect tool's outline, widened to a prefix in want, holds sub; the
// inspect session ends either way.
func (x *rfEnv) drawnScan(pts []image.Point, sub string, want ...string) (image.Rectangle, bool) {
	x.t.Helper()
	x.inspectOn()
	defer func() {
		x.a.insp.Toggle()
		x.frame()
	}()
	for _, q := range pts {
		if label, r, ok := x.drawnAt(q, want...); ok && strings.Contains(label, sub) {
			return r, true
		}
	}
	return image.Rectangle{}, false
}

// toolbarReply hovers row ts and returns the hover toolbar's Reply in
// thread button as drawn, the pointer on it. The card is a row of 32px
// buttons 4px in (quick reactions, then Add reaction, Reply in thread,
// ...); the outline names only Add reaction's
// (MessageActionButtonReaction), and Reply in thread is the next one.
func (x *rfEnv) toolbarReply(ts string) image.Point {
	x.t.Helper()
	r, ok := x.row(ts)
	if !ok {
		x.t.Fatalf("row %s isn't on screen", ts)
	}
	cv := x.cv()
	in := rowPoint(r, cv.ListRect())
	x.move(f32.Pt(float32(in.X), float32(in.Y)))
	x.settle(300 * time.Millisecond)
	tts, tb, ok := cv.ToolbarRect()
	if !ok || tts != ts {
		x.t.Fatalf("hovering row %s at %v drew the toolbar over %q (%v)", ts, in, tts, ok)
	}
	const pad, bs = 4, 32
	var pts []image.Point
	for px := tb.Min.X + pad + bs/2; px < tb.Max.X-pad; px += bs {
		pts = append(pts, image.Pt(px, (tb.Min.Y+tb.Max.Y)/2))
	}
	react, ok := x.drawnScan(pts, "MessageActionButtonReaction", "MessageActionButtonReaction")
	if !ok {
		x.t.Fatalf("no Add reaction button (MessageActionButtonReaction) drawn on the toolbar %v", tb)
	}
	p := image.Pt(react.Max.X+bs/2, (react.Min.Y+react.Max.Y)/2)
	if !p.In(tb) {
		x.t.Fatalf("Add reaction %v is the toolbar's (%v) last button", react, tb)
	}
	x.move(f32.Pt(float32(p.X), float32(p.Y)))
	return p
}

// replyBar returns the root's reply bar ("2 replies") as drawn.
func (x *rfEnv) replyBar() image.Point {
	x.t.Helper()
	r, ok := x.row(x.root)
	if !ok {
		x.t.Fatal("the root isn't on screen")
	}
	// the 34px bar ends 4px over the row's 4px bottom padding: its middle
	// about 25px up; then the points around it
	var pts []image.Point
	for d := 0; d <= 24; d += 4 {
		pts = append(pts, image.Pt(r.Min.X+120, r.Max.Y-25+d), image.Pt(r.Min.X+120, r.Max.Y-25-d))
	}
	// the outline is the ThreadReplies component's, its root element the bar
	bar, ok := x.drawnScan(pts[1:], "", "ThreadReplies", "div.c-message__reply_bar")
	if !ok {
		x.t.Fatalf("no reply bar drawn in the root's row %v", r)
	}
	return image.Pt(bar.Min.X+min(60, bar.Dx()/3), (bar.Min.Y+bar.Max.Y)/2)
}

// clickMods is a primary click with modifiers (Slack's split click).
func (x *rfEnv) clickMods(p image.Point, mods key.Modifiers) {
	pt := f32.Pt(float32(p.X), float32(p.Y))
	at := x.tick()
	x.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt, Time: at, Modifiers: mods},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt, Time: at + 10*time.Millisecond, Modifiers: mods},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt, Time: at + 60*time.Millisecond, Modifiers: mods},
	)
	x.frame()
	x.frame()
}

func (x *rfEnv) click(p image.Point) {
	x.clickAt(p, x.tick())
	x.frame()
	x.frame()
}

// Right-click a channel message, press T (Karim's case): the thread opens
// with its reply box focused.
func TestThreadsAuditReplyFocusMenuT(t *testing.T) {
	x := newRFEnv(t, false, false)
	x.openMenu(x.root)
	x.key("T", 0)
	x.waitThreadBox("the thread with its reply box")
	x.settle(300 * time.Millisecond)
	x.wantThreadFocus("context menu, T")
}

// The same with the thread's box cached from an earlier open, so it draws
// in the first frame after T: its focus has to stay over the menu
// layer's give-back to its opener (slackc layers.go closeUnseen).
func TestThreadsAuditReplyFocusMenuTCached(t *testing.T) {
	x := newRFEnv(t, false, false)
	x.a.OpenThread(x.ch, x.root)
	x.waitThreadBox("the thread opened first")
	x.a.CloseSecondary()
	x.settle(200 * time.Millisecond)
	x.focusComposer(x.ch, "")
	x.openMenu(x.root)
	x.key("T", 0)
	x.frame() // the menu runs T
	for k := 0; k < 4 && !x.threadFocused(); k++ {
		x.frame()
	}
	for k := 1; k <= 3; k++ {
		if !x.threadFocused() {
			t.Errorf("frame %d after the box took the focus: the thread's box lost it", k)
		}
		x.frame()
	}
	x.waitThreadBox("the thread with its reply box")
	x.settle(300 * time.Millisecond)
	x.wantThreadFocus("context menu, T, cached thread")
}

// Clicking the menu's Reply in thread row is the same as T.
func TestThreadsAuditReplyFocusMenuClick(t *testing.T) {
	x := newRFEnv(t, false, false)
	x.openMenu(x.root)
	var row image.Rectangle
	x.until("the menu's rows", 5*time.Second, func() bool {
		m := x.topMenu()
		if m == nil {
			return false
		}
		i := menuIndex(m.Items(), "comment")
		rs := m.RowRects()
		if i < 0 || len(rs) != len(m.Items()) || rs[i].Empty() {
			return false
		}
		row = rs[i]
		return true
	})
	x.click(row.Min.Add(row.Max).Div(2))
	x.waitThreadBox("the thread with its reply box")
	x.settle(300 * time.Millisecond)
	x.wantThreadFocus("context menu, Reply in thread clicked")
}

// The hover toolbar's Reply in thread (RwAt.onComment) focuses the box.
func TestThreadsAuditReplyFocusToolbar(t *testing.T) {
	x := newRFEnv(t, true, false)
	p := x.toolbarReply(x.root)
	x.click(p)
	x.waitThreadBox("the thread with its reply box")
	x.settle(300 * time.Millisecond)
	x.wantThreadFocus("hover toolbar, Reply in thread")
}

// On a broadcast reply the toolbar keys focusInput by its thread_ts, so
// the parent thread's box focuses.
func TestThreadsAuditReplyFocusToolbarBroadcast(t *testing.T) {
	x := newRFEnv(t, true, true)
	p := x.toolbarReply(x.bc)
	x.click(p)
	x.waitThreadBox("the broadcast's thread with its reply box")
	x.settle(300 * time.Millisecond)
	x.wantThreadFocus("hover toolbar on a broadcast reply")
}

// The channel list's menu passes no threadTs, so T on a broadcast reply
// keys focusInput by the reply's ts and its parent's box stays unfocused.
func TestThreadsAuditReplyFocusMenuBroadcastNot(t *testing.T) {
	x := newRFEnv(t, false, true)
	x.openMenu(x.bc)
	x.key("T", 0)
	x.waitThreadBox("the broadcast's thread with its reply box")
	x.settle(300 * time.Millisecond)
	x.wantNoThreadFocus("context menu T on a broadcast reply")
}

// T on the thread already open: nothing remounts, nothing refocuses.
func TestThreadsAuditReplyFocusMenuOpenThreadNot(t *testing.T) {
	x := newRFEnv(t, false, false)
	x.a.OpenThread(x.ch, x.root)
	x.waitThreadBox("the thread opened first")
	x.settle(200 * time.Millisecond)
	x.focusComposer(x.ch, "")
	x.openMenu(x.root)
	x.key("T", 0)
	x.settle(400 * time.Millisecond)
	x.wantNoThreadFocus("context menu T on the open thread")
}

// A permalink or a notification (Navigate to the thread route) sets no
// focusInput.
func TestThreadsAuditReplyFocusPermalinkNot(t *testing.T) {
	x := newRFEnv(t, false, false)
	x.a.Navigate("c/" + x.ch + "/t/" + x.root)
	x.waitThreadBox("the thread with its reply box")
	x.settle(300 * time.Millisecond)
	x.wantNoThreadFocus("the thread's route")
}

// Closing the pane cancels its thread's focusInput (cancelFocusInput on
// ThreadsFlexpane's unmount): a permalink to it afterwards doesn't focus.
func TestThreadsAuditReplyFocusClearedOnClose(t *testing.T) {
	x := newRFEnv(t, false, false)
	x.openMenu(x.root)
	x.key("T", 0)
	x.waitThreadBox("the thread with its reply box")
	x.settle(300 * time.Millisecond)
	if !x.threadFocused() {
		t.Fatal("T didn't focus the thread's box")
	}
	x.a.CloseSecondary()
	x.settle(200 * time.Millisecond)
	x.focusComposer(x.ch, "")
	x.a.Navigate("c/" + x.ch + "/t/" + x.root)
	x.waitThreadBox("the thread again")
	x.settle(300 * time.Millisecond)
	x.wantNoThreadFocus("the thread's route after the pane closed")
}

// The reply bar's openThread sets focusInput: the box focuses.
func TestThreadsAuditReplyFocusReplyBar(t *testing.T) {
	x := newRFEnv(t, true, false)
	p := x.replyBar()
	x.click(p)
	x.waitThreadBox("the thread with its reply box")
	x.settle(300 * time.Millisecond)
	x.wantThreadFocus("reply bar")
}

// The reply bar on the thread already open: nothing remounts, so the box
// isn't refocused.
func TestThreadsAuditReplyFocusReplyBarOpenNot(t *testing.T) {
	x := newRFEnv(t, true, false)
	x.a.OpenThread(x.ch, x.root)
	x.waitThreadBox("the thread opened first")
	x.settle(200 * time.Millisecond)
	p := x.replyBar()
	x.focusComposer(x.ch, "")
	x.click(p)
	x.settle(300 * time.Millisecond)
	x.wantNoThreadFocus("reply bar on the open thread")
}

// The reply bar's split click (⌘⌥, Ctrl+Alt) opens the split view and
// returns before setFocusInput (r0Yq.js:271-279): opening the thread in
// the side pane afterwards from its route leaves its box unfocused (no
// main-window conversation was left holding a focus request).
func TestThreadsAuditReplyFocusReplyBarSplitNot(t *testing.T) {
	x := newRFEnv(t, true, false)
	p := x.replyBar()
	x.clickMods(p, shortcut.Mod()|key.ModAlt)
	route := Route("c/" + x.ch + "/t/" + x.root)
	x.until("the thread in the split view", 8*time.Second, func() bool { return x.a.splitTop() == route })
	x.settle(300 * time.Millisecond)
	x.a.CloseSplit()
	x.settle(200 * time.Millisecond)
	x.focusComposer(x.ch, "")
	x.a.Navigate(string(route))
	x.waitThreadBox("the thread in the side pane")
	x.settle(300 * time.Millisecond)
	x.wantNoThreadFocus("the side pane's thread after the reply bar's split click")
}

// The toolbar's Reply with the split modifier opens the thread in the
// split view with focusInput set (0yz+ sets it on the tile branch too).
// Closing the split unmounts that ThreadsFlexpane, which cancels the key:
// the thread's route afterwards opens it in the side pane unfocused.
func TestThreadsAuditReplyFocusToolbarSplitNot(t *testing.T) {
	x := newRFEnv(t, true, false)
	p := x.toolbarReply(x.root)
	x.clickMods(p, shortcut.Mod()|key.ModAlt)
	route := Route("c/" + x.ch + "/t/" + x.root)
	x.until("the thread in the split view", 8*time.Second, func() bool { return x.a.splitTop() == route })
	x.settle(300 * time.Millisecond)
	x.a.CloseSplit()
	x.settle(200 * time.Millisecond)
	x.focusComposer(x.ch, "")
	x.a.Navigate(string(route))
	x.waitThreadBox("the thread in the side pane")
	x.settle(300 * time.Millisecond)
	x.wantNoThreadFocus("the side pane's thread after the toolbar's split Reply and the split closed")
}

// The reply bar inside the split view's channel opens the thread there
// with focusInput set; going back to the channel unmounts its
// ThreadsFlexpane and cancels the key, so the thread's route in the main
// window opens it unfocused.
func TestThreadsAuditReplyFocusSplitReplyBarNot(t *testing.T) {
	x := newRFEnv(t, true, false)
	x.a.OpenSplit("c/" + x.ch)
	x.inSplit = true
	x.until("the root in the split view", 15*time.Second, func() bool { _, ok := x.row(x.root); return ok })
	x.settle(300 * time.Millisecond)
	p := x.replyBar()
	x.click(p)
	route := Route("c/" + x.ch + "/t/" + x.root)
	x.until("the thread in the split view", 8*time.Second, func() bool { return x.a.splitTop() == route })
	x.settle(300 * time.Millisecond)
	x.a.inPane(paneSplit, x.a.GoPaneBack)
	x.until("the split view back on the channel", 5*time.Second, func() bool { return x.a.splitTop() == Route("c/"+x.ch) })
	x.settle(200 * time.Millisecond)
	x.focusComposer(x.ch, "")
	x.a.Navigate(string(route))
	x.waitThreadBox("the thread in the side pane")
	x.settle(300 * time.Millisecond)
	x.wantNoThreadFocus("the side pane's thread after the split view's reply bar and Back")
}
