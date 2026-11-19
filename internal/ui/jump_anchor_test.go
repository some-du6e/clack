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
	"testing"
	"time"

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
	want := e.anchorLine()
	for dl := time.Now().Add(d); time.Now().Before(dl); {
		e.frame()
		rows := e.paneRows("", ch, thread)
		if y, ok := topOf(rows, ts); !ok || y != want {
			t.Fatalf("%s: %s drawn %v with its top %dpx under the list's top (list %dpx tall); want it on the anchor line, %dpx (rows %s)",
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

// TestJumpToReplyOnAnchorLine: a link to a reply deep in a thread opens
// the thread pane with the reply on the anchor line, highlighted, and it
// stays there while the replies above it gain pictures.
func TestJumpToReplyOnAnchorLine(t *testing.T) {
	e := anchorEnv(t)
	ch, root := fakeslack.TombstoneChannelID, fakeslack.TombstoneRootTS
	reply := fakeslack.TombstoneReplyTS(19)
	e.a.OpenAtMessage(ch, reply, root, true)
	e.waitDrawn(t, "the thread at the reply, highlighted", 20*time.Second, "", ch, root, func(rows []messages.SenderView) bool { return lit(rows, reply) })
	e.holdsLine(t, "after the jump", ch, root, reply, time.Second)
	for i := 16; i <= 18; i++ {
		ts := fakeslack.TombstoneReplyTS(i)
		e.growWithPicture(ch, slack.Message{TS: ts, User: e.fs.UserIDs()[1+i%3], ThreadTS: root, Text: fmt.Sprintf("Thread reply %d", i+1)},
			fmt.Sprintf("anchor-reply-%d", i))
	}
	e.holdsLine(t, "while the replies above it grew", ch, root, reply, 2*time.Second)
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
	far := fakeslack.TombstoneOlderTS(fakeslack.TombstoneOlder - 70) // two pages past the window's
	for dl := time.Now().Add(40 * time.Second); !drewRow(e.paneRows("", ch, ""), far); {
		if time.Now().After(dl) {
			t.Fatalf("scrolling up from the root never drew %s, 70 messages before it: drew %s; conversations.history %q",
				far, drawnState(e.paneRows("", ch, "")), calls.of("conversations.history", ch, "latest", "oldest", "limit"))
		}
		e.scroll(e.primaryPoint(), -1500)
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
