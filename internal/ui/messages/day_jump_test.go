package messages

// The day pill's "The very beginning" in a channel whose newest page is
// far from it: jumpToDate is jumpToTimestamp at the channel's creation,
// which shows the slice holding that date (window_end_test.go's rule), so
// the list opens the window around the beginning and draws the channel's
// first message. It used to walk back from the newest page a page at a
// time and stop at the 20-page cap, landing on the oldest page it had
// loaded rather than the beginning. Synthetic history on the fake.

import (
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const dayJumpChan = "C0DAYJUMP01"

// TestVeryBeginningFromNewest: "The very beginning" from the newest
// messages, with more history than the paging cap held, shows the first
// message in the window around it.
func TestVeryBeginningFromNewest(t *testing.T) {
	sz := fakeslack.Small
	sz.History = 0 // the channel's history is only what this test posts
	app, fs, _ := shotsAppSize(t, sz)
	base := time.Now().Add(-36 * time.Hour).Unix()
	created := base - 60 // the channel's creation, a minute before its first message
	app.S.PutConv(slack.Conversation{ID: dayJumpChan, Name: "day-jump", IsChannel: true, IsMember: true, NumMembers: 12, Created: created})
	e := &winEnv{h: &ctxHost{coreHost: coreHost{app: app, th: theme.New(false)}}}
	// more than the newest page (50) plus 20 pages of 100: the paging cap
	// stopped short of the beginning
	for i := 0; i < 2100; i++ {
		ts := fmt.Sprintf("%d.000100", base+int64(i)*60)
		fs.PostQuietAt(dayJumpChan, "UBEN00001", fmt.Sprintf("message %d", i), ts)
		e.tss = append(e.tss, ts)
	}
	e.v = NewChannelView(e.h, dayJumpChan)
	e.rt = newRouted(e.v, 900, 700)
	e.await(t, "the newest messages", func() bool { return e.drawn(e.newest()) && e.v.AtBottom() })

	e.v.s.jumpToTime(e.h, time.Unix(created, 0)) // the menu item's Do
	e.await(t, "the channel's beginning", func() bool {
		return e.v.InWindow() && e.v.s.snap.loaded && !e.v.s.snap.hasMore &&
			e.v.s.pendingDate.IsZero() && e.drawn(e.tss[0])
	})
}
