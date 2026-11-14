package ui

// Your own new message through the window (W43): Enter in a channel's
// composer, with the list at its unread line or read back in its
// history, brings the list to the sent message, its row right over the
// composer (Slack's sendMessage jumpToTimestamp, messages.placeSendJump);
// Enter in a long thread's reply box brings the pane to its end, the
// reply 12px over the box (ThreadView.JumpToOwnSend). Someone else's
// message while you read back leaves the list where it is.

import (
	"encoding/json"
	"fmt"
	"image"
	"strconv"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/messages"
)

// ownSendList is ch's message list.
func (e *perfEnv) ownSendList(ch string) *messages.ChannelView {
	e.tb.Helper()
	if c := e.a.convos[ch+"/"]; c != nil {
		if l, ok := c.list.(channelList); ok {
			return l.ChannelView
		}
	}
	e.tb.Fatalf("no message list for %s", ch)
	return nil
}

// sentTS is the ts core holds for the newest message with text in ch.
func (e *perfEnv) sentTS(ch, text string) string {
	if tl := e.a.data.core.S.Timeline(ch); tl != nil {
		for i := len(tl.Msgs) - 1; i >= 0; i-- {
			if tl.Msgs[i].Text == text {
				return tl.Msgs[i].TS
			}
		}
	}
	return ""
}

// newestShown is the newest message row on screen (the bottom spacer
// isn't one).
func newestShown(rows []messages.RowY) (messages.RowY, bool) {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Key != "bottomSpacer" {
			return rows[i], true
		}
	}
	return messages.RowY{}, false
}

// sendOwn types text into ch's composer and presses Enter, then waits for
// the list to settle on the confirmed message and checks it is the newest
// row, slackGap over the composer.
func (e *perfEnv) sendOwn(t *testing.T, ch, text string) {
	t.Helper()
	e.channelComposer(ch) // clicked: the focus
	e.typeText(text)
	e.frame()
	e.key(key.NameReturn, 0)
	e.frame()
	v := e.ownSendList(ch)
	e.until("the list at the sent message", 15*time.Second, func() bool {
		r, ok := newestShown(v.VisibleRows())
		ts := e.sentTS(ch, text)
		return ok && ts != "" && r.Key == ts && v.AtBottom()
	})
	e.settle(300 * time.Millisecond)
	if !v.AtBottom() {
		t.Errorf("the list left the sent message")
	}
	bottom, ok := v.NewestRowBottom()
	if c := e.a.convos[ch+"/"]; !ok || c.compRect.Min.Y-bottom != slackGap {
		t.Errorf("the sent message ends at %d (on screen %v), %d px over the composer, want %d", bottom, ok, c.compRect.Min.Y-bottom, slackGap)
	}
}

func TestOwnSendJumpsToNewest(t *testing.T) {
	t.Run("history read back", func(t *testing.T) {
		fs, err := fakeslack.Start(fakeslack.Small)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { fs.Close() })
		e := startEnv(t, fs, t.TempDir())
		e.waitReady()
		ch := fs.ChannelIDs()[2]
		e.openChannel(ch)
		e.until("the channel's history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
		e.settle(800 * time.Millisecond)
		e.toLatest()
		for i := 0; i < 6; i++ {
			e.scroll(e.listPoint(), -400)
			e.frame()
		}
		e.settle(500 * time.Millisecond)
		v := e.ownSendList(ch)
		if v.AtBottom() {
			t.Fatal("the wheel didn't scroll the list up")
		}

		// someone else's message: the list stays where you read (its ts
		// is now, as the fake's chat.postMessage stamps the next one)
		first := v.VisibleRows()[0]
		now := time.Now()
		ts := fmt.Sprintf("%d.%06d", now.Unix(), now.Nanosecond()/1000)
		raw, _ := json.Marshal(map[string]any{"type": "message", "channel": ch, "user": "U0FAKE00009", "text": "meanwhile", "ts": ts, "event_ts": ts})
		e.a.data.core.Inject(slack.Event{Type: "message", Raw: raw, Channel: json.RawMessage(strconv.Quote(ch))})
		e.until("their message", 10*time.Second, func() bool { return e.sentTS(ch, "meanwhile") == ts })
		e.settle(300 * time.Millisecond)
		if got := v.VisibleRows()[0]; got != first {
			t.Errorf("their message moved the list: %+v, before %+v", got, first)
		}
		if v.AtBottom() {
			t.Errorf("their message took the list to its newest")
		}

		e.sendOwn(t, ch, "sent while reading history")
	})

	t.Run("the unread line", func(t *testing.T) {
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
		e.openChannel(ch)
		e.until("the channel's history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
		e.settle(800 * time.Millisecond)
		v := e.ownSendList(ch)
		line := false
		for _, r := range v.VisibleRows() {
			line = line || r.Key == "unread"
		}
		if !line || v.AtBottom() {
			t.Fatalf("#unreads-launch didn't open at its unread line (the line on screen %v, at the newest %v)", line, v.AtBottom())
		}
		e.sendOwn(t, ch, "sent from the unread line")
	})
}

func TestOwnReplyJumpsToTheEnd(t *testing.T) {
	e := threadsAuditEnv(t, false)
	ch, root := fakeslack.ThreadsAuditChannel, fakeslack.ThreadsAuditLong
	e.openChannel(ch)
	e.a.OpenThread(ch, root)
	var tv *messages.ThreadView
	e.until("the thread's replies", 15*time.Second, func() bool {
		tv = e.threadView(ch, root)
		rs, _ := e.a.data.core.S.Thread(ch, root)
		return tv != nil && len(rs) >= 40 && !replyBoxRect(e.a.convoFor(ch, root)).Empty()
	})
	e.settle(800 * time.Millisecond)
	if _, ok := tv.NewestRowBottom(); ok {
		t.Fatal("the 40-reply thread you don't follow opened with its newest reply on screen")
	}
	c := e.a.convoFor(ch, root)
	box := replyBoxRect(c)
	e.click(box.Min.Add(image.Pt(60, box.Dy()/2)))
	e.frame()
	const text = "a reply from the reply box"
	e.typeText(text)
	e.frame()
	e.key(key.NameReturn, 0)
	e.frame()
	sent := func() bool {
		rs, _ := e.a.data.core.S.Thread(ch, root)
		return len(rs) > 0 && rs[len(rs)-1].Text == text
	}
	e.until("the reply at the end", 15*time.Second, func() bool {
		_, ok := tv.NewestRowBottom()
		return ok && sent()
	})
	e.settle(300 * time.Millisecond)
	bottom, ok := tv.NewestRowBottom()
	box = replyBoxRect(c)
	if !ok || box.Min.Y-bottom != slackThreadGap {
		t.Errorf("the reply ends at %d (on screen %v), %d px over the reply box, want %d", bottom, ok, box.Min.Y-bottom, slackThreadGap)
	}
}
