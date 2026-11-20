package ui

// Karim's α.108 reports (Windows, 1920x1080 at 1x, 2026-11-18):
//
//   - "clicking message links or stuff like this highlight the message and
//     sometimes its shit at scrolling to a message": the highlight ran and
//     the message wasn't where Slack puts it, or wasn't on screen at all;
//   - "i cant scroll up from this thread even tho theres other stuff": a
//     thread opened from an archived channel ("You are viewing a thread
//     from an archived channel"), its root deleted (a tombstone, 40
//     replies); the conversation beside it showed only the root, and
//     scrolling up loaded nothing.
//
// Slack's message list scrolls a jumped-to message to its anchor line,
// ANCHOR_OFFSET (64px; getMessageListLayout, boot-shared IY25) under the
// list's top: scrollToKey for a message it holds and the layout's anchor
// for one it loads (AnchorLayout.getOffsetForKey, client-boot 62rv), both
// within the content's ends (bracketScrollTop). AnchorLayout keeps the
// message there while rows above it change height, items come and go and
// the list resizes, until the reader scrolls (MessageList.onScroll drops
// the anchor; K+Na). Synthetic fakeslack history; nothing talks to Slack.

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/messages"
)

// anchorEnv is longEnv with the archived tombstone thread
// (fakeslack.TombstoneThread).
func anchorEnv(t *testing.T) *perfEnv {
	t.Helper()
	sz := fakeslack.Small
	sz.ThreadHeader, sz.ReadCursors, sz.TombstoneThread = true, true, true
	sz.LongChannels, sz.LongHistory = 3, 3000
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	return e
}

// anchorLine is ANCHOR_OFFSET in the env's px.
func (e *perfEnv) anchorLine() int {
	s := e.scale
	if s == 0 {
		s = 1
	}
	return int(math.Round(64 * float64(s)))
}

// topOf is row ts's top under its list's top, as the last frame drew it.
func topOf(rows []messages.SenderView, ts string) (int, bool) {
	for _, r := range rows {
		if r.TS == ts {
			return r.Top, true
		}
	}
	return 0, false
}

func bodyOf(rows []messages.SenderView, ts string) image.Rectangle {
	for _, r := range rows {
		if r.TS == ts {
			return r.Body
		}
	}
	return image.Rectangle{}
}

// holdsLine fails unless every frame for d draws ts (pane ch/thread) with
// its top on the anchor line.
func (e *perfEnv) holdsLine(t *testing.T, what, ch, thread, ts string, d time.Duration) {
	t.Helper()
	e.holdsAt(t, what, ch, thread, ts, e.anchorLine(), d)
}

// px is dp in the env's px.
func (e *perfEnv) px(dp float64) int {
	s := e.scale
	if s == 0 {
		s = 1
	}
	return int(math.Round(dp * float64(s)))
}

// holdsAt fails unless every frame for d draws ts (pane ch/thread) with
// its top want px under the list's top.
func (e *perfEnv) holdsAt(t *testing.T, what, ch, thread, ts string, want int, d time.Duration) {
	t.Helper()
	for dl := time.Now().Add(d); time.Now().Before(dl); {
		e.frame()
		rows := e.paneRows("", ch, thread)
		if y, ok := topOf(rows, ts); !ok || y != want {
			t.Fatalf("%s: %s drawn %v with its top %dpx under the list's top (list %dpx tall); want %dpx (rows %s)",
				what, ts, ok, y, viewHOf(rows), want, drawnState(rows))
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func viewHOf(rows []messages.SenderView) int {
	if len(rows) > 0 {
		return rows[0].ViewH
	}
	return 0
}

// growWithPicture has the message ts of ch gain a 360x240 picture
// (message_changed with an image attachment, as an unfurl lands after the
// message): its row grows by the picture. m is the message as the fake
// holds it.
func (e *perfEnv) growWithPicture(ch string, m slack.Message, seed string) {
	msg := map[string]any{"type": "message", "ts": m.TS, "user": m.User, "text": m.Text,
		"attachments": []any{map[string]any{"id": 1, "fallback": "a synthetic picture",
			"image_url": e.fs.URL + "img/360x240/" + seed + ".png", "image_width": 360, "image_height": 240, "image_bytes": 4000}}}
	if m.ThreadTS != "" {
		msg["thread_ts"] = m.ThreadTS
	}
	now := fmt.Sprintf("%d.%06d", time.Now().Unix(), time.Now().Nanosecond()/1000)
	raw, _ := json.Marshal(map[string]any{"type": "message", "subtype": "message_changed", "channel": ch, "hidden": true,
		"ts": now, "event_ts": now, "message": msg,
		"previous_message": map[string]any{"type": "message", "ts": m.TS, "user": m.User, "text": m.Text}})
	e.a.Core().Inject(slack.Event{Type: "message", Channel: json.RawMessage(strconv.Quote(ch)), Raw: raw})
}

// primaryPoint is a point in the conversation's message list, left of an
// open secondary pane.
func (e *perfEnv) primaryPoint() image.Point {
	o, right := e.a.primaryOrigin, e.size.X
	if e.a.secondary.open() && e.a.secondaryOrigin.X > o.X {
		right = e.a.secondaryOrigin.X
	}
	return image.Pt((o.X+right)/2, o.Y+(e.size.Y-o.Y)/3)
}

// secondaryPoint is a point in the secondary pane's message list.
func (e *perfEnv) secondaryPoint() image.Point {
	o := e.a.secondaryOrigin
	return image.Pt((o.X+e.size.X)/2, o.Y+(e.size.Y-o.Y)/2)
}

// TestJumpToLoadedMessageOnAnchorLine: a link to a message the
// conversation holds, a few screens up, scrolls it to the anchor line
// (scrollToKey) and keeps it there. Clack put the message two rows under
// the list's top, wherever those rows' heights left it.
func TestJumpToLoadedMessageOnAnchorLine(t *testing.T) {
	e := anchorEnv(t)
	ch := longRead
	newest, _ := e.fs.HistoryAt(ch, 0)
	target := plainAt(t, e.fs, ch, 30)
	e.openChannel(ch)
	e.waitDrawn(t, "the newest messages", 20*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, newest.TS) })
	e.a.Navigate("c/" + ch + "/m/" + target)
	e.waitDrawn(t, "the message, highlighted", 15*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return lit(rows, target) })
	e.holdsLine(t, "after the jump", ch, "", target, 1500*time.Millisecond)
}

// TestJumpToLateMessageOnAnchorLine: a link to a message far back whose
// window answers late (both conversations.history calls held 1.5 s) lands
// with the message on the anchor line once it's in, and it stays there.
func TestJumpToLateMessageOnAnchorLine(t *testing.T) {
	e := anchorEnv(t)
	ch := longRead
	newest, _ := e.fs.HistoryAt(ch, 0)
	target := plainAt(t, e.fs, ch, 2500)
	e.openChannel(ch)
	e.waitDrawn(t, "the newest messages", 20*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, newest.TS) })
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "conversations.history" && f.Get("channel") == ch && (f.Get("latest") != "" || f.Get("oldest") != "") {
			time.Sleep(1500 * time.Millisecond) // the window, late
		}
		return nil, false
	})
	t.Cleanup(func() { e.fs.SetHook(nil) })
	e.a.Navigate("c/" + ch + "/m/" + target)
	e.waitDrawn(t, "the old message, highlighted", 30*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return lit(rows, target) })
	e.holdsLine(t, "after the late window", ch, "", target, 1500*time.Millisecond)
}

// TestJumpHoldsThroughRowsGrowingAbove: once the jump's message is on the
// anchor line, the rows above it gaining a picture (an unfurl landing)
// don't move it: AnchorLayout's setHeight keeps the anchor where it is.
// The reader's wheel then drops the anchor (onScroll): the list moves
// with the wheel and stays where it was scrolled to. Clack kept the first
// message on screen in place instead, so the pictures pushed the jump's
// message down the list, or off it.
func TestJumpHoldsThroughRowsGrowingAbove(t *testing.T) {
	e := anchorEnv(t)
	ch := longRead
	newest, _ := e.fs.HistoryAt(ch, 0)
	k := 30
	var target string
	for ; k < 130; k++ { // a message, and the three above it, no thread's parent
		ok := true
		for j := k; j <= k+3; j++ {
			if m, found := e.fs.HistoryAt(ch, j); !found || m.ThreadTS != "" {
				ok = false
			}
		}
		if ok {
			m, _ := e.fs.HistoryAt(ch, k)
			target = m.TS
			break
		}
	}
	if target == "" {
		t.Fatal("no run of four plain messages")
	}
	e.openChannel(ch)
	e.waitDrawn(t, "the newest messages", 20*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, newest.TS) })
	e.a.Navigate("c/" + ch + "/m/" + target)
	e.waitDrawn(t, "the message, highlighted", 15*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return lit(rows, target) })
	e.holdsLine(t, "after the jump", ch, "", target, 500*time.Millisecond)
	above, _ := e.fs.HistoryAt(ch, k+1)
	before := bodyOf(e.paneRows("", ch, ""), above.TS).Dy()
	top0, _ := topOf(e.paneRows("", ch, ""), above.TS)
	for j := k + 1; j <= k+3; j++ {
		m, _ := e.fs.HistoryAt(ch, j)
		e.growWithPicture(ch, m, fmt.Sprintf("anchor-above-%d", j))
	}
	want := e.anchorLine()
	grew := false
	for dl := time.Now().Add(10 * time.Second); !grew; {
		if time.Now().After(dl) {
			t.Fatalf("the row above the message never drew its picture: body %dpx tall (%dpx before)", bodyOf(e.paneRows("", ch, ""), above.TS).Dy(), before)
		}
		e.frame()
		rows := e.paneRows("", ch, "")
		if y, ok := topOf(rows, target); !ok || y != want {
			t.Fatalf("while the rows above grew: %s drawn %v at %dpx under the list's top; want %dpx (rows %s)", target, ok, y, want, drawnState(rows))
		}
		// the picture is in: the row's body is taller, so its top is higher
		top, _ := topOf(rows, above.TS)
		grew = bodyOf(rows, above.TS).Dy() >= before+e.anchorLine()*3 || top <= top0-e.anchorLine()*3
		time.Sleep(2 * time.Millisecond)
	}
	e.holdsLine(t, "after the rows above grew", ch, "", target, time.Second)
	// the reader's wheel: the anchor goes, and the list stays scrolled
	e.scroll(e.listPoint(), -200)
	e.settle(300 * time.Millisecond)
	y0, ok := topOf(e.paneRows("", ch, ""), target)
	if !ok || y0 <= want+100 {
		t.Fatalf("after a 200px wheel up: %s drawn %v at %dpx; want it scrolled down from %dpx", target, ok, y0, want)
	}
	e.settle(time.Second)
	if y, ok := topOf(e.paneRows("", ch, ""), target); !ok || y != y0 {
		t.Errorf("after the wheel: %s moved from %dpx to %dpx (drawn %v); want it left where the reader scrolled it", target, y0, y, ok)
	}
}

// TestJumpToReplyOnAnchorLine is the thread list's start for a link to
// a reply deep in a thread with no unread replies: getThreadStartPosition's
// startOffset 100, the reply 100px under the pane's top (ThreadsFlexpane,
// boot-shared 4M8N), highlighted. Nothing holds it there after (the
// anchor drops at the scroll's echo, and the thread list's own line is
// its top): a reply between the top and it that gains a picture pushes it
// down. Clack put the reply 64px down and held it there.
func TestJumpToReplyOnAnchorLine(t *testing.T) {
	e := anchorEnv(t)
	ch, root := fakeslack.TombstoneChannelID, fakeslack.TombstoneRootTS
	reply := fakeslack.TombstoneReplyTS(19)
	e.a.OpenAtMessage(ch, reply, root, true)
	e.waitDrawn(t, "the thread at the reply, highlighted", 20*time.Second, "", ch, root, func(rows []messages.SenderView) bool { return lit(rows, reply) })
	want := e.px(100)
	e.holdsAt(t, "after the jump", ch, root, reply, want, time.Second)
	above := fakeslack.TombstoneReplyTS(18)
	if y, ok := topOf(e.paneRows("", ch, root), above); !ok || y < 0 {
		t.Fatalf("the reply above isn't wholly on screen under the pane's top: %d (%v)", y, ok)
	}
	e.growWithPicture(ch, slack.Message{TS: above, User: e.fs.UserIDs()[1+18%3], ThreadTS: root, Text: "Thread reply 19"}, "thread-reply-above")
	for dl := time.Now().Add(10 * time.Second); ; {
		y, ok := topOf(e.paneRows("", ch, root), reply)
		if ok && y >= want+e.px(150) {
			break // pushed down by the picture above it
		}
		if time.Now().After(dl) {
			t.Fatalf("the reply above gained a picture and the reply stayed at %d (%v): held, where Slack's anchor has dropped", y, ok)
		}
		e.frame()
		time.Sleep(2 * time.Millisecond)
	}
}

// TestThreadReHighlightScrollsLazilyToTop: in a pane already showing the
// thread, a link to a reply off screen scrolls its top to the pane's top
// (a new highlightKey: scrollToKey(startTs, {lazy: true}), the line at the
// top), and a link to a reply whose top is on screen doesn't scroll.
// Clack placed every re-highlight 64px down. (From the same conversation
// beside the pane, jumpToThreadReply doesn't flash the reply.)
func TestThreadReHighlightScrollsLazilyToTop(t *testing.T) {
	e := anchorEnv(t)
	ch, root := fakeslack.TombstoneChannelID, fakeslack.TombstoneRootTS
	e.a.OpenAtMessage(ch, fakeslack.TombstoneReplyTS(30), root, true)
	e.waitDrawn(t, "the thread at reply 31", 20*time.Second, "", ch, root, func(rows []messages.SenderView) bool {
		return lit(rows, fakeslack.TombstoneReplyTS(30))
	})
	e.settle(3500 * time.Millisecond) // the flash done
	off := fakeslack.TombstoneReplyTS(5)
	if _, ok := topOf(e.paneRows("", ch, root), off); ok {
		t.Fatalf("reply 6 is on screen already")
	}
	e.a.OpenAtMessage(ch, off, root, true)
	for dl := time.Now().Add(10 * time.Second); ; {
		y, ok := topOf(e.paneRows("", ch, root), off)
		if ok && y == 0 {
			break
		}
		if time.Now().After(dl) {
			t.Fatalf("a link to reply 6, off screen, left it at %d (%v); want its top at the pane's top", y, ok)
		}
		e.frame()
		time.Sleep(2 * time.Millisecond)
	}
	e.settle(500 * time.Millisecond)
	on := fakeslack.TombstoneReplyTS(7)
	y0, ok := topOf(e.paneRows("", ch, root), on)
	if !ok || y0 <= 0 {
		t.Fatalf("reply 8 isn't on screen under the top: %d (%v)", y0, ok)
	}
	e.a.OpenAtMessage(ch, on, root, true)
	e.settle(800 * time.Millisecond)
	if y, ok := topOf(e.paneRows("", ch, root), on); !ok || y != y0 {
		t.Errorf("a link to reply 8, on screen, scrolled it from %d to %d (%v); want it left where it was", y0, y, ok)
	}
}

// TestArchivedTombstoneWindowPagesOlder is Karim's capture: a link to a
// thread in an archived channel whose root is a tombstone. The
// conversation beside the thread shows the window around the root, and
// scrolling up from it loads the pages before it (requestOlder, latest
// the window's start) and draws them.
func TestArchivedTombstoneWindowPagesOlder(t *testing.T) {
	e := anchorEnv(t)
	ch, root := fakeslack.TombstoneChannelID, fakeslack.TombstoneRootTS
	calls := e.recordCalls("conversations.history")
	e.a.OpenAtMessage(ch, root, root, true)
	e.waitDrawn(t, "the tombstone root in the conversation", 20*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, root) })
	e.waitDrawn(t, "the thread", 20*time.Second, "", ch, root, func(rows []messages.SenderView) bool { return drewRow(rows, fakeslack.TombstoneReplyTS(0)) })
	// two pages past the window's: a row at or before it drawn (a wheel
	// can carry the list past it between frames)
	far := fakeslack.TombstoneOlderTS(fakeslack.TombstoneOlder - 70)
	reached := func(rows []messages.SenderView) bool {
		for _, r := range rows {
			if r.TS == far || slackTSBefore(r.TS, far) {
				return true
			}
		}
		return false
	}
	for dl := time.Now().Add(40 * time.Second); !reached(e.paneRows("", ch, "")); {
		if time.Now().After(dl) {
			t.Fatalf("scrolling up from the root never drew %s, 70 messages before it, or anything older: drew %s; conversations.history %q",
				far, drawnState(e.paneRows("", ch, "")), calls.of("conversations.history", ch, "latest", "oldest", "limit"))
		}
		e.scroll(e.primaryPoint(), -600)
		e.frame()
		time.Sleep(20 * time.Millisecond)
	}
}

// TestArchivedTombstoneThreadScrollsUp: the tombstone's thread pane (the
// route of Karim's capture, c/<channel>/t/<root>), scrolled to its newest
// reply, scrolls up to its first reply and its root.
func TestArchivedTombstoneThreadScrollsUp(t *testing.T) {
	e := anchorEnv(t)
	ch, root := fakeslack.TombstoneChannelID, fakeslack.TombstoneRootTS
	first, last := fakeslack.TombstoneReplyTS(0), fakeslack.TombstoneReplyTS(fakeslack.TombstoneReplies-1)
	e.a.Navigate("c/" + ch + "/t/" + root)
	e.waitDrawn(t, "the thread", 20*time.Second, "", ch, root, func(rows []messages.SenderView) bool { return len(rows) > 3 })
	tv := e.threadView(ch, root)
	if tv == nil {
		t.Fatal("no thread pane")
	}
	tv.ScrollToBottom()
	e.waitDrawn(t, "the thread at its newest reply", 10*time.Second, "", ch, root, func(rows []messages.SenderView) bool {
		return drewRow(rows, last) && !drewRow(rows, first)
	})
	for dl := time.Now().Add(30 * time.Second); ; {
		rows := e.paneRows("", ch, root)
		if drewRow(rows, first) && drewRow(rows, root) {
			break
		}
		if time.Now().After(dl) {
			t.Fatalf("scrolling up the thread never reached its first reply %s and root: drew %s", first, drawnState(rows))
		}
		e.scroll(e.secondaryPoint(), -400)
		e.frame()
		time.Sleep(10 * time.Millisecond)
	}
}

// TestJumpWindowEndStaysPut: a jump window doesn't keep to its end
// (MessageList.setStickToBottom(reachedEnd || prevReachedEnd), and a
// window slice hasn't reachedEnd): wheeled to its end, the newer page that
// lands goes in under the reader, who stays where they were, and no more
// pages follow without input. Clack kept every list to its end, so each
// newer page pulled the reader to the new end, which asked for the next
// page a second later, until the window ran into the newest messages.
func TestJumpWindowEndStaysPut(t *testing.T) {
	e := anchorEnv(t)
	ch := longRead
	target := plainAt(t, e.fs, ch, 2500)
	newest, _ := e.fs.HistoryAt(ch, 0)
	e.openChannel(ch)
	e.waitDrawn(t, "the newest messages", 20*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, newest.TS) })
	e.a.Navigate("c/" + ch + "/m/" + target)
	e.waitDrawn(t, "the old message, highlighted", 30*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return lit(rows, target) })
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "conversations.history" && f.Get("channel") == ch && f.Get("oldest") != "" && f.Get("latest") == "" {
			time.Sleep(400 * time.Millisecond) // the newer page, a moment late
		}
		return nil, false
	})
	t.Cleanup(func() { e.fs.SetHook(nil) })
	calls := e.recordCalls("conversations.history")
	newer := func() int {
		k := 0
		for _, c := range calls.of("conversations.history", ch, "oldest", "latest") {
			if c != "oldest= latest=" && strings.HasSuffix(c, " latest=") {
				k++
			}
		}
		return k
	}
	for dl := time.Now().Add(15 * time.Second); newer() < 1; {
		if time.Now().After(dl) {
			t.Fatalf("wheeled to the window's end, no newer page was asked: %q", calls.of("conversations.history", ch, "latest", "oldest"))
		}
		e.scroll(e.listPoint(), 3000)
		e.frame()
		time.Sleep(10 * time.Millisecond)
	}
	e.frame()
	rows := e.paneRows("", ch, "")
	if len(rows) == 0 {
		t.Fatal("nothing drawn at the window's end")
	}
	last := rows[len(rows)-1]
	e.settle(3 * time.Second) // the page lands; no input
	y, ok := topOf(e.paneRows("", ch, ""), last.TS)
	if !ok || y != last.Top {
		t.Errorf("the newer page moved the reader: %s, the last row in view, went from %d to %d (drawn %v)", last.TS, last.Top, y, ok)
	}
	if n := newer(); n > 1 {
		t.Errorf("%d newer pages in 3 s with no input; want the one the reader's scroll asked for", n)
	}
}

// TestJumpWindowLastPageStaysPut: the newer page that meets the newest
// messages lands under the reader like the pages before it. The window
// around a message 90 back stops short of the newest page (the newest
// 50), and its first newer page reaches them: core joins the two, and
// the list keeps to its end from then on (reachedEnd). Clack turned that
// on before the stick test of the same sync, so a reader at the window's
// end was taken to the page's end, 27 messages on. Slack's setKeys reads
// stickToBottom from before: MessageList.componentDidUpdate sets it after
// the children's setKeys (AEjl:1163; dxC+ setKeys).
func TestJumpWindowLastPageStaysPut(t *testing.T) {
	e := anchorEnv(t)
	ch := longRead
	target := plainAt(t, e.fs, ch, 90)
	newest, _ := e.fs.HistoryAt(ch, 0)
	e.openChannel(ch)
	e.waitDrawn(t, "the newest messages", 20*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, newest.TS) })
	e.a.Navigate("c/" + ch + "/m/" + target)
	e.waitDrawn(t, "the message 90 back, highlighted", 30*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return lit(rows, target) })
	if cv := e.channelView(ch); cv == nil || !cv.InWindow() {
		t.Fatal("the jump 90 back didn't show a window short of the newest messages")
	}
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "conversations.history" && f.Get("channel") == ch && f.Get("oldest") != "" && f.Get("latest") == "" {
			time.Sleep(300 * time.Millisecond) // the newer page, a moment late
		}
		return nil, false
	})
	t.Cleanup(func() { e.fs.SetHook(nil) })
	calls := e.recordCalls("conversations.history")
	newer := func() int {
		k := 0
		for _, c := range calls.of("conversations.history", ch, "oldest", "latest") {
			if c != "oldest= latest=" && strings.HasSuffix(c, " latest=") {
				k++
			}
		}
		return k
	}
	for dl := time.Now().Add(15 * time.Second); newer() < 1; {
		if time.Now().After(dl) {
			t.Fatalf("wheeled to the window's end, no newer page was asked: %q", calls.of("conversations.history", ch, "latest", "oldest"))
		}
		e.scroll(e.listPoint(), 3000)
		e.frame()
		time.Sleep(10 * time.Millisecond)
	}
	e.frame()
	rows := e.paneRows("", ch, "")
	if len(rows) == 0 {
		t.Fatal("nothing drawn at the window's end")
	}
	last := rows[len(rows)-1]
	e.settle(3 * time.Second) // the page lands and meets the newest messages; no input
	rows = e.paneRows("", ch, "")
	if y, ok := topOf(rows, last.TS); !ok || y != last.Top {
		t.Errorf("the page that met the newest messages moved the reader: %s, the last row in view, went from %d to %d (drawn %v; %s)",
			last.TS, last.Top, y, ok, drawnState(rows))
	}
	if drewRow(rows, newest.TS) {
		t.Errorf("the newest message is drawn after the last page landed: %s", drawnState(rows))
	}
}

// TestAnchorLineKeepsRowAfterScroll is AnchorLayout's setHeight once the
// reader has scrolled: a row ending above the anchor line (64px down) that
// grows moves the list by what it grew, so the row on the line stays where
// it is. Clack kept the first message wholly on screen in place instead,
// so a short row at the top gaining a picture pushed everything under it
// down.
func TestAnchorLineKeepsRowAfterScroll(t *testing.T) {
	e := anchorEnv(t)
	ch := longRead
	newest, _ := e.fs.HistoryAt(ch, 0)
	e.openChannel(ch)
	e.waitDrawn(t, "the newest messages", 20*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, newest.TS) })
	e.settle(500 * time.Millisecond)
	e.scroll(e.listPoint(), -2000) // read back, with room under the screen
	e.settle(500 * time.Millisecond)
	if cv := e.channelView(ch); cv == nil || cv.AtBottom() {
		t.Fatalf("a 2000px wheel up left the list at its end")
	}
	line := e.anchorLine()
	// a short row on screen, then wheeled to the list's top: it ends above
	// the anchor line
	var short messages.SenderView
	rows := e.paneRows("", ch, "")
	for i := 0; i+1 < len(rows); i++ {
		if h := rows[i+1].Top - rows[i].Top; rows[i].Top > 0 && h > 0 && h < line-8 {
			short = rows[i]
			break
		}
	}
	if short.TS == "" {
		t.Fatalf("no row under %dpx tall on screen: %s", line-8, drawnState(rows))
	}
	e.scroll(e.listPoint(), float32(short.Top))
	e.settle(400 * time.Millisecond)
	rows = e.paneRows("", ch, "")
	if y, ok := topOf(rows, short.TS); !ok || y != 0 {
		t.Fatalf("the short row %s isn't at the list's top: %d (%v)", short.TS, y, ok)
	}
	var onLine messages.SenderView
	for _, r := range rows {
		if r.Top <= line && r.Top+1 > 0 && r.TS != short.TS && r.Top > 0 {
			onLine = r // the row across or at the line
		}
	}
	if onLine.TS == "" {
		t.Fatalf("no row on the anchor line: %s", drawnState(rows))
	}
	k := -1
	for i := 0; i < 3000; i++ {
		if m, ok := e.fs.HistoryAt(ch, i); ok && m.TS == short.TS {
			k = i
			break
		}
	}
	m, _ := e.fs.HistoryAt(ch, k)
	e.growWithPicture(ch, m, "anchor-short-row")
	for dl := time.Now().Add(10 * time.Second); ; {
		rows := e.paneRows("", ch, "")
		if y, ok := topOf(rows, short.TS); ok && y < -e.anchorLine() {
			break // it grew upward, past the list's top
		}
		if time.Now().After(dl) {
			y, ok := topOf(rows, onLine.TS)
			t.Fatalf("the short row's picture never pushed it up past the list's top; the row on the line %s went from %d to %d (%v)", onLine.TS, onLine.Top, y, ok)
		}
		e.frame()
		time.Sleep(2 * time.Millisecond)
	}
	if y, ok := topOf(e.paneRows("", ch, ""), onLine.TS); !ok || y != onLine.Top {
		t.Errorf("the row on the anchor line %s moved from %d to %d (%v) when a row above the line grew", onLine.TS, onLine.Top, y, ok)
	}
}

// TestNewMessageNearEndSticks is AnchorLayout's stick-to-bottom inside
// STICKY_EPSILON (getMessageListLayout: half the default row height, 40px
// in the light theme): read back 20px from the end, a new message arrives
// and the list keeps to its end, the new message on screen. Clack stuck
// only when the list was exactly at its end, so the message landed below
// the fold.
func TestNewMessageNearEndSticks(t *testing.T) {
	e := anchorEnv(t)
	ch := longRead
	newest, _ := e.fs.HistoryAt(ch, 0)
	e.openChannel(ch)
	e.waitDrawn(t, "the newest messages", 20*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, newest.TS) })
	e.settle(500 * time.Millisecond)
	cv := e.channelView(ch)
	if cv == nil || !cv.AtBottom() {
		t.Fatal("the conversation didn't open at its end")
	}
	e.scroll(e.listPoint(), -20)
	e.settle(300 * time.Millisecond)
	if cv.AtBottom() {
		t.Fatal("a 20px wheel up left the list at its end")
	}
	ts := fmt.Sprintf("%d.000600", time.Now().Unix())
	raw, _ := json.Marshal(map[string]any{"type": "message", "channel": ch, "user": e.fs.UserIDs()[3], "text": "synthetic new message near the end", "ts": ts})
	e.a.Core().Inject(slack.Event{Type: "message", Channel: json.RawMessage(strconv.Quote(ch)), Raw: raw})
	e.waitDrawn(t, "the new message on screen", 10*time.Second, "", ch, "", func(rows []messages.SenderView) bool {
		y, ok := topOf(rows, ts)
		return ok && y < viewHOf(rows)
	})
	if !cv.AtBottom() {
		t.Errorf("20px from the end when a message arrived: the list didn't keep to its end")
	}
}

// TestNewMessageNearEndSticksDarkClean: STICKY_EPSILON is half the
// default row height of the messages theme, 40px Clean and 30px Compact
// (getMessageListLayout's "light" or "dense", MessagePane's xp33), not the
// color mode: in dark mode a Clean list 35px from its end keeps to it when
// a message arrives. Clack took 30px in dark mode.
func TestNewMessageNearEndSticksDarkClean(t *testing.T) {
	e := anchorEnv(t)
	e.a.th.SetDark(true)
	ch := longRead
	newest, _ := e.fs.HistoryAt(ch, 0)
	e.openChannel(ch)
	e.waitDrawn(t, "the newest messages", 20*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, newest.TS) })
	e.settle(500 * time.Millisecond)
	cv := e.channelView(ch)
	if cv == nil || !cv.AtBottom() {
		t.Fatal("the conversation didn't open at its end")
	}
	e.scroll(e.listPoint(), -float32(e.px(35)))
	e.settle(300 * time.Millisecond)
	if cv.AtBottom() {
		t.Fatal("a 35px wheel up left the list at its end")
	}
	ts := fmt.Sprintf("%d.000700", time.Now().Unix())
	raw, _ := json.Marshal(map[string]any{"type": "message", "channel": ch, "user": e.fs.UserIDs()[3], "text": "synthetic new message 35px from the end", "ts": ts})
	e.a.Core().Inject(slack.Event{Type: "message", Channel: json.RawMessage(strconv.Quote(ch)), Raw: raw})
	e.waitDrawn(t, "the new message on screen", 10*time.Second, "", ch, "", func(rows []messages.SenderView) bool {
		y, ok := topOf(rows, ts)
		return ok && y < viewHOf(rows)
	})
	if !cv.AtBottom() {
		t.Errorf("dark mode, Clean, 35px from the end when a message arrived: the list didn't keep to its end")
	}
}

// TestEmptyComposerPageKeys: MessagePane's document keydown hands PageUp,
// PageDown, Home and End to the message list while the channel's input
// has the focus and is empty (AEjl's onKeyDown): PageUp from the empty
// composer pages the list up, End takes it back to its end. They moved
// the composer's caret, which had nowhere to go.
func TestEmptyComposerPageKeys(t *testing.T) {
	e := anchorEnv(t)
	ch := longRead
	newest, _ := e.fs.HistoryAt(ch, 0)
	e.openChannel(ch)
	e.waitDrawn(t, "the newest messages", 20*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, newest.TS) })
	e.settle(500 * time.Millisecond)
	e.focusComposer(ch, "")
	e.settle(200 * time.Millisecond)
	if f, ok := e.a.convoFor(ch, "").comp.(interface{ Focused() bool }); !ok || !f.Focused() {
		t.Fatal("the composer didn't take the focus")
	}
	cv := e.channelView(ch)
	if cv == nil {
		t.Fatal("no channel view")
	}
	// the list's own PageUp, as the composer hands it over (PageKey)
	cv.PageKey("PageUp")
	e.settle(300 * time.Millisecond)
	if cv.AtBottom() || drewRow(e.paneRows("", ch, ""), newest.TS) {
		t.Fatalf("the list's PageUp (PageKey) didn't page it up: %s", drawnState(e.paneRows("", ch, "")))
	}
	e.key(key.NameEnd, 0)
	e.settle(300 * time.Millisecond)
	if !cv.AtBottom() || !drewRow(e.paneRows("", ch, ""), newest.TS) {
		t.Fatalf("End in the empty composer didn't take the list to its end: %s", drawnState(e.paneRows("", ch, "")))
	}
	e.key(key.NamePageUp, 0)
	e.settle(300 * time.Millisecond)
	if cv.AtBottom() || drewRow(e.paneRows("", ch, ""), newest.TS) {
		t.Errorf("PageUp in the empty composer didn't page the list up: %s", drawnState(e.paneRows("", ch, "")))
	}
}

// betweenTS is a ts between messages k and k+1 back in ch (no message
// has it: a message deleted since, a reply the history never holds), and
// the newer of the two, the message a jump to it lands on.
func (e *perfEnv) betweenTS(t *testing.T, ch string, k int) (missing, next string) {
	t.Helper()
	newer, ok1 := e.fs.HistoryAt(ch, k)
	older, ok2 := e.fs.HistoryAt(ch, k+1)
	if !ok1 || !ok2 {
		t.Fatalf("no messages %d and %d back in %s", k, k+1, ch)
	}
	a, _ := strconv.ParseFloat(older.TS, 64)
	b, _ := strconv.ParseFloat(newer.TS, 64)
	if b-a < 0.002 {
		t.Fatalf("messages %d and %d back in %s are too close for a ts between", k, k+1, ch)
	}
	return strconv.FormatFloat(a+0.001, 'f', 6, 64), newer.TS
}

// TestJumpToMissingMessageLandsOnNext: a link to a message no row holds
// (deleted since) far back in a conversation not loaded yet lands, once
// the window around it is in, on the first message after it, on the
// anchor line (Slack's findTimestampClosestToStartTs: find(timestamps,
// t => t >= startTs)), and nothing is left pending. Clack matched only
// the exact ts, gave the jump up and left the window drawn from its end.
func TestJumpToMissingMessageLandsOnNext(t *testing.T) {
	e := anchorEnv(t)
	ch := "C0FAKE00003" // read: not loaded
	missing, next := e.betweenTS(t, ch, 600)
	e.a.Navigate("c/" + ch + "/m/" + missing)
	e.waitDrawn(t, "the message after the missing one", 30*time.Second, "", ch, "", func(rows []messages.SenderView) bool {
		y, ok := topOf(rows, next)
		return ok && y == e.anchorLine()
	})
	if cv := e.channelView(ch); cv == nil || cv.JumpPending() != "" {
		t.Errorf("the jump is still pending after it landed")
	}
	e.holdsLine(t, "after the jump to a missing message", ch, "", next, time.Second)
}

// TestJumpToMissingInWindowDoesNotClimb: inside a window, a jump to a ts
// no row holds lands on the next message and is done: scroll anchoring
// is on again, so one older page landing above leaves the reader where
// they were, and no more pages follow without input. Clack left such a
// jump pending for good with the list's anchoring off, so each page that
// landed above pulled the view to its top, which asked for the next page,
// climbing to the conversation's first message.
func TestJumpToMissingInWindowDoesNotClimb(t *testing.T) {
	e := anchorEnv(t)
	ch := longRead
	target := plainAt(t, e.fs, ch, 2500)
	e.openAtOld(t, ch, target)
	missing, next := e.betweenTS(t, ch, 2492) // inside the window, newer than the target
	e.a.Navigate("c/" + ch + "/m/" + missing)
	e.waitDrawn(t, "the message after the missing one", 15*time.Second, "", ch, "", func(rows []messages.SenderView) bool {
		y, ok := topOf(rows, next)
		return ok && y == e.anchorLine()
	})
	if cv := e.channelView(ch); cv == nil || cv.JumpPending() != "" {
		t.Fatalf("the jump to a missing message is still pending")
	}
	calls := e.recordCalls("conversations.history")
	older := func() int {
		k := 0
		for _, c := range calls.of("conversations.history", ch, "latest") {
			if c != "latest=" {
				k++
			}
		}
		return k
	}
	for dl := time.Now().Add(15 * time.Second); older() < 1; {
		if time.Now().After(dl) {
			t.Fatalf("scrolling up the window asked for no older page")
		}
		e.scroll(e.listPoint(), -600)
		e.frame()
		time.Sleep(20 * time.Millisecond)
	}
	e.settle(4 * time.Second) // no input
	if n := older(); n > 1 {
		t.Errorf("%d older pages in 4 s after one scroll up: the list climbed by itself", n)
	}
}

// TestHighlightRootFromElsewhereAnchors: a thread opened from elsewhere
// (Activity's Open in home: toThread with highlightRoot) puts the
// conversation's root on the anchor line, wherever the conversation was
// left. MessageList's lazyScroll holds only when the primary was showing
// the same conversation with the root in the range it held (AEjl
// UNSAFE_componentWillReceiveProps: inRange and the same channelId).
// Clack applied the lazy rule from anywhere, against the layout the
// conversation was last drawn with: messages that arrived since had moved
// the root off screen, and the stale layout said it was on it.
func TestHighlightRootFromElsewhereAnchors(t *testing.T) {
	e := anchorEnv(t)
	ch, other := longRead, fakeslack.ThreadPrivateID
	users := e.fs.UserIDs()[1:4]
	root, _ := e.fs.PostQuiet(ch, users[0], "a synthetic thread root near the end")
	reply := e.fs.PostReplyQuiet(ch, root, users[1], "a synthetic reply")
	e.openChannel(ch)
	e.waitDrawn(t, "the root at the end", 20*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, root) })
	e.settle(300 * time.Millisecond)
	e.openChannel(other)
	e.settle(300 * time.Millisecond)
	base := time.Now().Unix() + 5
	for n := 0; n < 15; n++ { // new messages in the conversation meanwhile
		ts := fmt.Sprintf("%d.%06d", base+int64(n), 700)
		raw, _ := json.Marshal(map[string]any{"type": "message", "channel": ch, "user": users[n%3], "text": "a synthetic newer message", "ts": ts})
		e.a.Core().Inject(slack.Event{Type: "message", Channel: json.RawMessage(strconv.Quote(ch)), Raw: raw})
	}
	e.settle(300 * time.Millisecond)
	e.a.OpenAtMessage(ch, reply, root, true) // Activity's Open in home
	e.waitDrawn(t, "the root on the anchor line, highlighted", 15*time.Second, "", ch, "", func(rows []messages.SenderView) bool {
		y, ok := topOf(rows, root)
		return ok && y == e.anchorLine() && lit(rows, root)
	})
}

// TestEnteringWithUnreadsShowsNewest: after a jump that stayed among the
// newest messages, entering the conversation again once it has unreads
// shows the newest messages at the end (determineTimestamps clears the
// scrollmark when it has unreads; no anchor survives a channel switch).
// Clack kept the old place, the jump's message still on the line and the
// new messages below the fold.
func TestEnteringWithUnreadsShowsNewest(t *testing.T) {
	e := anchorEnv(t)
	ch, other := longRead, fakeslack.ThreadPrivateID
	newest, _ := e.fs.HistoryAt(ch, 0)
	target := plainAt(t, e.fs, ch, 30)
	e.openChannel(ch)
	e.waitDrawn(t, "the newest messages", 20*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, newest.TS) })
	e.a.Navigate("c/" + ch + "/m/" + target)
	e.waitDrawn(t, "the message on the anchor line", 15*time.Second, "", ch, "", func(rows []messages.SenderView) bool {
		y, ok := topOf(rows, target)
		return ok && y == e.anchorLine()
	})
	e.openChannel(other)
	e.settle(300 * time.Millisecond)
	ts := fmt.Sprintf("%d.000800", time.Now().Unix()+3)
	raw, _ := json.Marshal(map[string]any{"type": "message", "channel": ch, "user": e.fs.UserIDs()[3], "text": "a synthetic new message", "ts": ts})
	e.a.Core().Inject(slack.Event{Type: "message", Channel: json.RawMessage(strconv.Quote(ch)), Raw: raw})
	e.until("the conversation unread", 10*time.Second, func() bool { return e.a.Core().Count(ch).Unread })
	e.openChannel(ch)
	e.waitDrawn(t, "the newest messages, the new one in them", 10*time.Second, "", ch, "", func(rows []messages.SenderView) bool {
		y, ok := topOf(rows, ts)
		return ok && y < viewHOf(rows)
	})
	if cv := e.channelView(ch); cv == nil || !cv.AtBottom() {
		t.Errorf("entered with unreads: the list isn't at its end")
	}
}

// TestUnreadConversationOpensAtNewest: with Mark as read's default
// prefs ("Start me at the newest message, but leave unseen messages
// unread": mark_msgs_read_immediately and start_scroll_at_oldest off) a
// conversation with unread messages opens at its newest, however many
// are unread, and so does entering it again from elsewhere.
// determineTimestamps clears an unread conversation's scrollmark and adds
// no startTs (client-boot eCxs:1838-1863; dYJr gives one only with both
// prefs), and the pane anchors at the end (dxC+ getOffsetForKey(null));
// the unread banner jumps up to the "New" line. Clack opened at the "New"
// line once more than 12 items followed it (a rule from the first WIP).
func TestUnreadConversationOpensAtNewest(t *testing.T) {
	sz := fakeslack.Small
	sz.Unreads = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	ch := fakeslack.UnreadsLaunchID // 31 unread messages
	newest, _ := fs.HistoryAt(ch, 0)
	e.openChannel(ch)
	e.waitDrawn(t, "#unreads-launch", 20*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return len(rows) > 0 })
	e.settle(800 * time.Millisecond)
	if cv := e.channelView(ch); cv == nil || !cv.AtBottom() || !drewRow(e.paneRows("", ch, ""), newest.TS) {
		t.Fatalf("#unreads-launch, 31 unread, didn't open at its newest message: %s", drawnState(e.paneRows("", ch, "")))
	}
	// read back a few screens, look elsewhere, come back: still unread
	for k := 0; k < 6; k++ {
		e.scroll(e.listPoint(), -400)
		e.frame()
	}
	e.settle(300 * time.Millisecond)
	if cv := e.channelView(ch); cv == nil || cv.AtBottom() {
		t.Fatal("wheeling up left the list at its newest")
	}
	e.openChannel(fakeslack.ThreadPrivateID)
	e.settle(300 * time.Millisecond)
	// 15 new messages while the reader is away
	last := ""
	for k := 0; k < 15; k++ {
		last = fmt.Sprintf("%d.%06d", time.Now().Unix()+5, 100+k)
		raw, _ := json.Marshal(map[string]any{"type": "message", "channel": ch, "user": e.fs.UserIDs()[3], "text": fmt.Sprintf("a synthetic new message %d", k), "ts": last})
		e.a.Core().Inject(slack.Event{Type: "message", Channel: json.RawMessage(strconv.Quote(ch)), Raw: raw})
	}
	e.until("#unreads-launch unread", 10*time.Second, func() bool { return e.a.Core().Count(ch).Unread })
	e.openChannel(ch)
	e.waitDrawn(t, "#unreads-launch's newest message", 10*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, last) })
	e.settle(500 * time.Millisecond)
	if cv := e.channelView(ch); cv == nil || !cv.AtBottom() || !drewRow(e.paneRows("", ch, ""), last) {
		t.Errorf("entered #unreads-launch again with its unreads: not at its newest message: %s", drawnState(e.paneRows("", ch, "")))
	}
}

// TestHighlightRootRouteKeepsWindow: a highlightRoot jump (toThread from
// elsewhere) into a conversation showing a window, with unreads, isn't an
// entry without a start: the primary gets the root as its startTs, so the
// window isn't left for the newest messages first (determineTimestamps
// clears the scrollmark only with no startTs). Clack entered the
// conversation as from the sidebar, drew its newest messages, then opened
// the window again.
func TestHighlightRootRouteKeepsWindow(t *testing.T) {
	e := anchorEnv(t)
	ch, other := longRead, fakeslack.ThreadPrivateID
	target := plainAt(t, e.fs, ch, 2500)
	newest, _ := e.fs.HistoryAt(ch, 0)
	e.openAtOld(t, ch, target)
	e.openChannel(other)
	e.settle(300 * time.Millisecond)
	ts := fmt.Sprintf("%d.000900", time.Now().Unix()+3)
	raw, _ := json.Marshal(map[string]any{"type": "message", "channel": ch, "user": e.fs.UserIDs()[3], "text": "a synthetic new message", "ts": ts})
	e.a.Core().Inject(slack.Event{Type: "message", Channel: json.RawMessage(strconv.Quote(ch)), Raw: raw})
	e.until("the conversation unread", 10*time.Second, func() bool { return e.a.Core().Count(ch).Unread })
	e.a.OpenAtMessage(ch, target, target, true) // the message as a thread's root, highlightRoot
	for dl := time.Now().Add(15 * time.Second); ; {
		rows := e.paneRows("", ch, "")
		if drewRow(rows, newest.TS) || drewRow(rows, ts) {
			t.Fatalf("the highlightRoot jump drew the newest messages first: %s", drawnState(rows))
		}
		if lit(rows, target) {
			break
		}
		if time.Now().After(dl) {
			t.Fatalf("the root never drew highlighted: %s", drawnState(rows))
		}
		e.frame()
		time.Sleep(time.Millisecond)
	}
	if cv := e.channelView(ch); cv == nil || !cv.InWindow() {
		t.Errorf("after the highlightRoot jump the list left the window")
	}
}

// TestPageKeysScrollTheList is BaseKeyboardNavigableList's PageUp and End
// (client-boot FOmE) with the message list focused: PageUp scrolls a page
// up and selects the first message in view, End goes back to the end and
// selects the last. Clack's list took only the arrows.
func TestPageKeysScrollTheList(t *testing.T) {
	e := anchorEnv(t)
	ch := longRead
	newest, _ := e.fs.HistoryAt(ch, 0)
	e.openChannel(ch)
	e.waitDrawn(t, "the newest messages", 20*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, newest.TS) })
	e.a.FocusMessage(ch, newest.TS)
	sel := func() string {
		if l, ok := e.a.convoFor(ch, "").list.(interface{ KeyboardSelected() string }); ok {
			return l.KeyboardSelected()
		}
		return ""
	}
	e.until("the list focused at the newest message", 10*time.Second, func() bool { return sel() == newest.TS })
	e.key(key.NamePageUp, 0)
	e.settle(300 * time.Millisecond)
	rows := e.paneRows("", ch, "")
	if drewRow(rows, newest.TS) {
		t.Fatalf("PageUp left the newest message on screen: %s", drawnState(rows))
	}
	if s := sel(); s == newest.TS || !drewRow(rows, s) {
		t.Errorf("after PageUp the selection is %s; want the first message in view (%s)", s, drawnState(rows))
	}
	e.key(key.NameEnd, 0)
	e.settle(300 * time.Millisecond)
	if cv := e.channelView(ch); cv == nil || !cv.AtBottom() || !drewRow(e.paneRows("", ch, ""), newest.TS) {
		t.Errorf("End didn't take the list back to its end")
	}
	if s := sel(); s != newest.TS {
		t.Errorf("after End the selection is %s; want the last message, %s", s, newest.TS)
	}
}

// TestArchivedTombstoneJumpScrollsBothWays is the test Karim's capture
// needed ("i cant scroll up from this thread even tho theres other
// stuff"): a link into an archived channel's thread whose root was
// deleted (a tombstone with 40 replies), the thread being the channel's
// last discussion, so the window around the root reaches the newest
// message and joins the conversation's newest messages. From the root on
// the anchor line, scrolling up loads and draws the pages before it, and
// scrolling back down reaches the channel's last message, its archive, at
// the list's end.
func TestArchivedTombstoneJumpScrollsBothWays(t *testing.T) {
	e := anchorEnv(t)
	ch, root := fakeslack.TombstoneNearID, fakeslack.TombstoneNearRootTS
	e.a.OpenAtMessage(ch, root, root, true)
	e.waitDrawn(t, "the tombstone root on the anchor line", 20*time.Second, "", ch, "", func(rows []messages.SenderView) bool {
		y, ok := topOf(rows, root)
		return ok && y == e.anchorLine()
	})
	far := fakeslack.TombstoneNearOlderTS(fakeslack.TombstoneOlder - 70) // two pages past the window's
	reached := func(rows []messages.SenderView) bool {
		for _, r := range rows {
			if r.TS == far || slackTSBefore(r.TS, far) {
				return true
			}
		}
		return false
	}
	calls := e.recordCalls("conversations.history")
	for dl := time.Now().Add(40 * time.Second); !reached(e.paneRows("", ch, "")); {
		if time.Now().After(dl) {
			t.Fatalf("scrolling up from the root never drew %s or anything older: drew %s; conversations.history %q",
				far, drawnState(e.paneRows("", ch, "")), calls.of("conversations.history", ch, "latest", "oldest", "limit"))
		}
		e.scroll(e.primaryPoint(), -600)
		e.frame()
		time.Sleep(20 * time.Millisecond)
	}
	archive := fakeslack.TombstoneNearArchiveTS()
	for dl := time.Now().Add(40 * time.Second); ; {
		rows := e.paneRows("", ch, "")
		if cv := e.channelView(ch); cv != nil && cv.AtBottom() && drewRow(rows, archive) {
			break
		}
		if time.Now().After(dl) {
			t.Fatalf("scrolling down never reached the channel's last message %s at the end: drew %s", archive, drawnState(rows))
		}
		e.scroll(e.primaryPoint(), 600)
		e.frame()
		time.Sleep(20 * time.Millisecond)
	}
}

// TestFlashEndsWhenRowLeaves: a jumped-to message scrolled out of the list
// while it flashes ends its flash for good (its MessageBackground unmounts:
// componentWillUnmount, rFp4), so scrolling straight back finds it plain.
// Clack's flash ran on the clock wherever the row went.
func TestFlashEndsWhenRowLeaves(t *testing.T) {
	e := anchorEnv(t)
	ch := longRead
	newest, _ := e.fs.HistoryAt(ch, 0)
	target := plainAt(t, e.fs, ch, 40)
	e.openChannel(ch)
	e.waitDrawn(t, "the newest messages", 20*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, newest.TS) })
	e.a.Navigate("c/" + ch + "/m/" + target)
	e.waitDrawn(t, "the message, highlighted", 15*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return lit(rows, target) })
	e.scroll(e.listPoint(), 4000) // away: the row leaves the list
	e.settle(150 * time.Millisecond)
	if drewRow(e.paneRows("", ch, ""), target) {
		t.Fatalf("a 4000px wheel down left the message on screen")
	}
	for dl := time.Now().Add(2 * time.Second); !drewRow(e.paneRows("", ch, ""), target); { // straight back, inside the 3 s
		if time.Now().After(dl) {
			t.Fatalf("scrolling back up never brought the message back on screen")
		}
		e.scroll(e.listPoint(), -300)
		e.frame()
		e.frame()
	}
	if rows := e.paneRows("", ch, ""); lit(rows, target) {
		t.Errorf("back on screen within 3 s, the message still flashes: its row left the list, which ends the flash")
	}
}
