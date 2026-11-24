package messages

// The message pane's DegradedBanner (degraded_banner.go): while the socket
// has trouble connecting it reads "Last updated less than a minute ago…"
// and "Load new messages", the unread pill hides under it, the text
// follows the 60 s interval, and the link hides it for 10 s and asks for
// the conversation's history and the counts again. Synthetic history on
// the fake.

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

func TestDegradedBannerLikeSlack(t *testing.T) {
	var trouble atomic.Bool
	prev := connectionTrouble
	connectionTrouble = func(Host) bool { return trouble.Load() }
	t.Cleanup(func() { connectionTrouble = prev })
	tick, hide := DegradedBannerTick, DegradedBannerHide
	DegradedBannerTick, DegradedBannerHide = 200*time.Millisecond, 400*time.Millisecond
	t.Cleanup(func() { DegradedBannerTick, DegradedBannerHide = tick, hide })

	const ch = "C0DEGRADE1"
	e, fs := guardEnv(t, ch, nil, func(app *core.App, _ *fakeslack.Server, tss []string) {
		app.S.SetCounts(&slack.Counts{Channels: []slack.CountEntry{{ID: ch, LastRead: tss[190], Latest: tss[239], HasUnreads: true}}})
	})
	b := e.v.DegradedBannerState()
	e.await(t, "control: the unread pill", func() bool { return e.v.s.ub.text != "" })
	if b.Text != "" {
		t.Fatalf("control: the banner shows with no trouble: %q", b.Text)
	}

	trouble.Store(true)
	e.await(t, "the banner", func() bool { return b.Text != "" })
	if b.Text != "Last updated less than a minute ago…" || b.Link != "Load new messages" {
		t.Errorf("the banner reads %q / %q", b.Text, b.Link)
	}
	if e.v.s.ub.text != "" {
		t.Errorf("the unread pill (%q) shows with the banner", e.v.s.ub.text)
	}
	if !strings.Contains(b.Aria, "Press F6 to navigate to the status banner") {
		t.Errorf("the aria-live line is %q", b.Aria)
	}
	// the interval: toTimeAgo of the mount, a minute back
	e.v.s.degraded.updated = e.v.s.degraded.updated.Add(-61 * time.Second)
	e.await(t, "the interval's text", func() bool { return b.Text == "Last updated 1 minute ago…" })

	hist, counts := fs.CallCount("conversations.history"), fs.CallCount("client.counts")
	e.rt.press(centre(b.LinkR), true)
	e.await(t, "the click hides the banner", func() bool { return b.Text == "" })
	e.await(t, "the history and the counts asked for again", func() bool {
		return fs.CallCount("conversations.history") > hist && fs.CallCount("client.counts") > counts
	})
	e.await(t, "back after the hide", func() bool { return b.Text != "" })

	trouble.Store(false)
	e.await(t, "gone with the trouble", func() bool { return b.Text == "" && e.v.s.ub.text != "" })
}
