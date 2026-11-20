package messages

// A jump's window lays out without ScrollToEnd (its end isn't the newest
// messages', so it keeps to none), and layout.Position{} is its top.
// Round 4 left the writers that meant the end with it, and each sent the
// list to the window's oldest rows: the day pill's "Most recent"
// (jumpToTime's zero time), ↓ onto the window's last message (scrollTo's
// newest case, also an edit of it and a date landing on it) and
// FocusNewest; a date past the window's end found nothing in it and was
// dropped. Slack's jumpToDate(null) and scrollToMostRecentMessage go from
// a window to the newest messages (jumpToTimestamp({ts: ""}) without
// reachedEnd, AEjl 1451-1461 and 1860-1883), a date is jumpToTimestamp at
// the date (the slice holding it), and scrollToKey of the last row goes to
// its offset. Synthetic history on the fake.

import (
	"fmt"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// winEndChan is winEnv's channel.
const winEndChan = "C0WINEND01"

// winEnv is a channel of 240 messages 90 s apart, its list in the window
// around the 41st (the newest page holds the last 50).
type winEnv struct {
	h   *ctxHost
	v   *ChannelView
	rt  *routed
	tss []string // oldest first
}

func newWinEnv(t *testing.T) *winEnv {
	t.Helper()
	e := newHistEnv(t)
	e.v.Highlight(e.tss[40])
	e.await(t, "the window around message 40", func() bool {
		return e.v.InWindow() && e.drawn(e.tss[40]) && e.v.s.pendingJump == ""
	})
	return e
}

// newHistEnv is winEnv's channel, its list at its newest messages.
func newHistEnv(t *testing.T) *winEnv {
	t.Helper()
	app, fs, _ := shotsApp(t)
	ch := winEndChan
	app.S.PutConv(slack.Conversation{ID: ch, Name: "archive-walk", IsChannel: true, IsMember: true, NumMembers: 12})
	e := &winEnv{h: &ctxHost{coreHost: coreHost{app: app, th: theme.New(false)}}}
	base := time.Now().Add(-7 * time.Hour).Unix()
	for i := 0; i < 240; i++ {
		ts := fmt.Sprintf("%d.000100", base+int64(i)*90)
		fs.PostQuietAt(ch, "UBEN00001", fmt.Sprintf("message %d", i), ts)
		e.tss = append(e.tss, ts)
	}
	e.v = NewChannelView(e.h, ch)
	e.rt = newRouted(e.v, 900, 700)
	e.await(t, "the newest messages", func() bool { return e.drawn(e.newest()) && e.v.AtBottom() })
	return e
}

func (e *winEnv) newest() string { return e.tss[len(e.tss)-1] }

// drawn reports whether the last layout drew message ts in view.
func (e *winEnv) drawn(ts string) bool {
	for _, r := range e.v.s.laid {
		if r.key == ts && r.y+r.h > 0 && r.y < e.v.s.size.Y {
			return true
		}
	}
	return false
}

// index is message ts's place in the history (-1: not a message).
func (e *winEnv) index(ts string) int {
	for i, x := range e.tss {
		if x == ts {
			return i
		}
	}
	return -1
}

// state is what the last layout drew: the window or not, and the messages
// in view.
func (e *winEnv) state() string {
	first, last := -1, -1
	for _, r := range e.v.s.laid {
		if i := e.index(r.key); i >= 0 && r.y+r.h > 0 && r.y < e.v.s.size.Y {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	return fmt.Sprintf("in a window %v, messages %d to %d in view, keyboard on message %d",
		e.v.InWindow(), first, last, e.index(e.v.KeyboardSelected()))
}

// await lays the list out until cond holds, for up to 10 s.
func (e *winEnv) await(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for dl := time.Now().Add(10 * time.Second); time.Now().Before(dl); time.Sleep(15 * time.Millisecond) {
		e.rt.frame()
		if cond() {
			return
		}
	}
	t.Fatalf("%s: %s", what, e.state())
}

// TestWindowMostRecentLeavesWindow: the day pill's "Most recent" in a
// window shows the newest messages, at the list's end.
func TestWindowMostRecentLeavesWindow(t *testing.T) {
	e := newWinEnv(t)
	e.v.s.jumpToTime(e.h, time.Time{}) // the menu item's Do
	e.await(t, `"Most recent" from the window`, func() bool { return !e.v.InWindow() && e.drawn(e.newest()) })
}

// TestWindowDownArrowOntoLastMessage: ↓ from the keyboard's message onto
// the window's last shows that message, in the window.
func TestWindowDownArrowOntoLastMessage(t *testing.T) {
	e := newWinEnv(t)
	m := e.v.s.snap.msgs
	last := m[len(m)-1].m.TS
	prev := e.tss[e.index(last)-1]
	e.v.FocusMessage(prev, func() {})
	e.await(t, "the keyboard on the message before the window's last", func() bool { return e.v.KeyboardSelected() == prev })
	for k := 0; k < 3; k++ { // the focus frame holds that frame's events
		e.rt.frame()
	}
	e.rt.r.Queue(key.Event{Name: key.NameDownArrow, State: key.Press}, key.Event{Name: key.NameDownArrow, State: key.Release})
	e.await(t, "↓ onto the window's last message", func() bool { return e.v.KeyboardSelected() == last && e.drawn(last) })
	if !e.v.InWindow() {
		t.Errorf("↓ onto the window's last message left the window: %s", e.state())
	}
}

// TestWindowFocusNewestLeavesWindow: FocusNewest (↑ in the empty composer
// with "Move focus to the message list") in a window selects the newest
// message, drawn, as scrollToMostRecentMessage(true) does.
func TestWindowFocusNewestLeavesWindow(t *testing.T) {
	e := newWinEnv(t)
	e.v.FocusNewest(func() {})
	e.await(t, "FocusNewest from the window", func() bool {
		return !e.v.InWindow() && e.v.KeyboardSelected() == e.newest() && e.drawn(e.newest())
	})
}

// TestWindowDateJumpPastItsEnd: a date past the window's end that the
// newest messages don't hold shows the window around it, from its first
// message at or after the date; one they hold shows them there.
func TestWindowDateJumpPastItsEnd(t *testing.T) {
	e := newWinEnv(t)
	mid := e.tss[120]
	e.v.s.jumpToTime(e.h, tsTime(mid))
	e.await(t, "a date between the window and the newest messages", func() bool { return e.v.InWindow() && e.drawn(mid) })
	recent := e.tss[215]
	e.v.s.jumpToTime(e.h, tsTime(recent))
	e.await(t, "a date the newest messages hold", func() bool { return !e.v.InWindow() && e.drawn(recent) })
}
