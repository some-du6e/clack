package messages

// Karim, 2026-11-16, scrolling up a busy channel: "why not prefetch".
// Slack's message list (BaseDynamicList, boot-shared akeQ) renders rows
// half a pane past the viewport (the virtual list's overscanRatio .5) and
// asks for the older page once the first of them starts within 400px of
// the list's top (loadMore's loadPre), counting a row it hasn't measured
// as 80px (getMessageListLayout's DEFAULT_HEIGHT for Clean; 60px for Compact). So
// the page loads while the reader has more than half a pane to scroll.
// Clack asked only with the third row on screen. Synthetic data.

import (
	"fmt"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestOlderPageAheadOfScrollLikeSlack(t *testing.T) {
	const ch = "C0AHEAD001"
	for _, c := range []struct {
		name  string
		first int  // the row at the viewport's top, none above it measured
		ask   bool // whether Slack's list asks for the older page
		dense bool // Compact: an unmeasured row is 60px
	}{
		// Compact: 11 rows of 60px, 660px over a 700px pane; at 80px
		// (Clean's) the overscan's first row would start past 400px
		{"eleven rows above, Compact", 11, true, true},
		// 6 rows of 80px over the viewport: the overscan reaches 480-h/2
		// (the pane's height h is over 160px), a row starting under 400px
		{"six rows above", 6, true, false},
		// 40 rows, 3200px: the overscan's first row is far past 400px
		{"forty rows above", 40, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			setDense(t, c.dense)
			app, _ := newTestApp(t)
			app.S.PutConv(slack.Conversation{ID: ch, Name: "busy", IsChannel: true, IsMember: true, NumMembers: 8})
			var msgs []slack.Message
			base := int64(1700300000)
			for i := 0; i < 120; i++ {
				user := []string{"UALICE", "UBOB"}[i%2]
				msgs = append(msgs, slack.Message{Type: "message", TS: fmt.Sprintf("%d.000100", base+int64(i)*3600), User: user,
					Text: fmt.Sprintf("synthetic message %d", i)})
			}
			app.S.SetLatest(ch, msgs, true) // more history before these
			v := NewChannelView(coreHost{app: app, th: theme.New(false)}, ch)
			f := frameFn(v)
			until(t, 5*time.Second, "the newest message", f, func() bool { return itemHeight(v.s, msgs[119].TS) > 0 })
			if !v.s.lastOlder.IsZero() || v.s.snap.older {
				t.Fatal("the older page was asked for at the newest messages: the case tests nothing")
			}
			// the reader scrolled up to row c.first (nothing above it laid out)
			v.s.list.Position = layout.Position{First: c.first, BeforeEnd: true}
			f()
			if got := v.s.list.Position.First; got != c.first {
				t.Fatalf("the list moved to row %d, not %d: the case tests nothing", got, c.first)
			}
			f()
			f()
			if asked := !v.s.lastOlder.IsZero() || v.s.snap.older; asked != c.ask {
				t.Errorf("with %d unmeasured rows over a %dpx pane the older page asked for: %v; Slack's list: %v",
					c.first, v.s.size.Y, asked, c.ask)
			}
		})
	}
}
