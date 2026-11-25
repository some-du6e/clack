package messages

// The day pill from the newest messages ("The very beginning" opens the
// window around the channel's start: jumpToTime, day_jump_test.go) and
// back with "Latest messages": the channel stays unread and an open
// inline edit keeps its text, as for a permalink's window
// (window_jump_guards_test.go).

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// dayGuardEnv is TestVeryBeginningFromNewest's channel (2100 messages a
// minute apart, only what the test posts), message i by self when
// self(i); setup runs before the view opens.
func dayGuardEnv(t *testing.T, self func(i int) bool, setup func(app *core.App, tss []string)) (*winEnv, *fakeslack.Server, int64) {
	t.Helper()
	sz := fakeslack.Small
	sz.History = 0
	app, fs, _ := shotsAppSize(t, sz)
	base := time.Now().Add(-36 * time.Hour).Unix()
	created := base - 60
	app.S.PutConv(slack.Conversation{ID: dayJumpChan, Name: "day-jump", IsChannel: true, IsMember: true, NumMembers: 12, Created: created})
	e := &winEnv{h: &ctxHost{coreHost: coreHost{app: app, th: theme.New(false)}}}
	for i := 0; i < 2100; i++ {
		ts := fmt.Sprintf("%d.000100", base+int64(i)*60)
		user, text := "UBEN00001", fmt.Sprintf("message %d", i)
		if self != nil && self(i) {
			user, text = fakeslack.SelfID, fmt.Sprintf("my draft note %d", i)
		}
		fs.PostQuietAt(dayJumpChan, user, text, ts)
		e.tss = append(e.tss, ts)
	}
	if setup != nil {
		setup(app, e.tss)
	}
	e.v = NewChannelView(e.h, dayJumpChan)
	e.rt = newRouted(e.v, 900, 700)
	e.await(t, "the newest messages", func() bool { return e.drawn(e.newest()) && e.v.AtBottom() })
	return e, fs, created
}

// beginningAndBack is "The very beginning" and "Latest messages" back.
func (e *winEnv) beginningAndBack(t *testing.T, created int64) {
	t.Helper()
	e.v.s.jumpToTime(e.h, time.Unix(created, 0)) // the menu item's Do
	e.await(t, "the channel's beginning", func() bool {
		return e.v.InWindow() && e.v.s.snap.loaded && !e.v.s.snap.hasMore &&
			e.v.s.pendingDate.IsZero() && e.drawn(e.tss[0])
	})
	e.frames(300 * time.Millisecond)
	e.await(t, `the "Latest messages" pill`, func() bool { return !e.v.LatestMessagesRect().Empty() })
	e.rt.press(centre(e.v.LatestMessagesRect()), true)
	e.await(t, `"Latest messages": the newest messages`, func() bool {
		return !e.v.InWindow() && e.drawn(e.newest()) && e.v.AtBottom()
	})
}

// TestVeryBeginningFromNewestKeepsUnread: the channel unread, its "New"
// line above the view; the beginning and back don't mark it read.
func TestVeryBeginningFromNewestKeepsUnread(t *testing.T) {
	e, fs, created := dayGuardEnv(t, nil, func(app *core.App, tss []string) {
		app.S.SetCounts(&slack.Counts{Channels: []slack.CountEntry{{ID: dayJumpChan, LastRead: tss[2052], Latest: tss[2099], HasUnreads: true}}})
	})
	// the newest page (Slack's 28) starts after the last read: the line
	// shows once the page before it, read ahead of the reader, reaches it
	// (canShowUnreadDivider)
	e.await(t, "the \"New\" line", func() bool { return e.v.s.unreadIndex() >= 0 })
	if e.v.s.unreadSeen {
		t.Fatalf("control: the newest messages' \"New\" line counts as seen: %s", e.state())
	}
	e.beginningAndBack(t, created)
	e.frames(2 * time.Second)
	if n := fs.CallCount("conversations.mark"); n != 0 {
		t.Errorf("\"The very beginning\" and \"Latest messages\" back marked the channel read (%d conversations.mark): %s", n, e.state())
	}
}

// TestVeryBeginningKeepsTheInlineEdit: an open edit with typed text; the
// beginning and back show it again with the text.
func TestVeryBeginningKeepsTheInlineEdit(t *testing.T) {
	e, _, created := dayGuardEnv(t, func(i int) bool { return i == 2095 }, nil)
	mine := e.tss[2095]
	e.v.Edit(mine)
	e.await(t, "the editor", func() bool {
		ed := e.v.EditComposer()
		return ed != nil && strings.Contains(ed.DocText(), "note 2095")
	})
	ed := e.v.EditComposer()
	ed.InsertText(" and more")
	want := ed.DocText()
	e.beginningAndBack(t, created)
	e.frames(300 * time.Millisecond)
	got := e.v.EditComposer()
	if got == nil {
		t.Fatalf("after the beginning and back the inline edit is gone: %s", e.state())
	}
	if got.DocText() != want {
		t.Errorf("after the beginning and back the editor holds %q, want %q", got.DocText(), want)
	}
}
