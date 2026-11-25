package views

// The Threads and Unreads views' first loads and offline states
// (messages.MessageContainerLoading, the offline indicators, build
// 889ac449a32d): "Loading threads…" (Unreads: "Loading unread messages…")
// and 5 s on "Still loading… sorry for the long wait!"; offline with
// nothing to show, LargeOfflineIndicator with "Go online to get the
// latest threads." (Unreads: the default "Go online to get the latest
// messages."). Synthetic data on internal/fakeslack.

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func shortStillLoading(t *testing.T) {
	t.Cleanup(setStillLoadingAfter(300 * time.Millisecond))
}

func drew(e *vaEnv, s string) bool {
	for _, x := range drawnTexts(e) {
		if strings.TrimSpace(x) == s {
			return true
		}
	}
	return false
}

func awaitDrawn(t *testing.T, e *vaEnv, s string) {
	t.Helper()
	for dl := time.Now().Add(10 * time.Second); !drew(e, s); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatalf("%q never drew (last frame: %q)", s, drawnTexts(e))
		}
	}
}

func TestThreadsSlowLoadLikeSlack(t *testing.T) {
	shortStillLoading(t)
	app, fs := vaFake(t)
	fs.SetFault("subscriptions.thread.getView", fakeslack.Fault{Delay: 3 * time.Second})
	v := NewThreads(&slickHost{app: app, th: theme.New(false)})
	e := &vaEnv{size: image.Pt(1000, 900)}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	awaitDrawn(t, e, "Loading threads…")
	awaitDrawn(t, e, "Still loading… sorry for the long wait!")

	// offline with no threads: the large indicator
	app2, fs2 := vaFake(t)
	fs2.SetFault("subscriptions.thread.getView", fakeslack.Fault{Delay: 3 * time.Second})
	t.Cleanup(setViewsOnline(func(Host) bool { return false }))
	v2 := NewThreads(&slickHost{app: app2, th: theme.New(false)})
	e2 := &vaEnv{size: image.Pt(1000, 900)}
	e2.draw = func(gtx layout.Context) { v2.Layout(gtx) }
	awaitDrawn(t, e2, "Go online to get the latest threads.")
	awaitDrawn(t, e2, "No connection")
}

func TestUnreadsSlowLoadLikeSlack(t *testing.T) {
	shortStillLoading(t)
	app, _ := vaFake(t) // no client.counts applied: the unread channels aren't known
	v := NewUnreads(&slickHost{app: app, th: theme.New(false)})
	e := &vaEnv{size: image.Pt(1000, 900)}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	awaitDrawn(t, e, "Loading unread messages…")
	awaitDrawn(t, e, "Still loading… sorry for the long wait!")

	t.Cleanup(setViewsOnline(func(Host) bool { return false }))
	app2, _ := vaFake(t)
	v2 := NewUnreads(&slickHost{app: app2, th: theme.New(false)})
	e2 := &vaEnv{size: image.Pt(1000, 900)}
	e2.draw = func(gtx layout.Context) { v2.Layout(gtx) }
	awaitDrawn(t, e2, "Go online to get the latest messages.")
}

// The Threads view's paging row (loading_more_threads): "Loading more
// messages…", 5 s on "Still loading… sorry for the long wait!", and
// offline InlineOfflineIndicator with "Go online to load more threads.".
func TestThreadsPagingRowLikeSlack(t *testing.T) {
	shortStillLoading(t)
	app, _ := vaFake(t)
	v := NewThreads(&slickHost{app: app, th: theme.New(false)})
	e := &vaEnv{size: image.Pt(1000, 200)}
	e.draw = func(gtx layout.Context) { threadsPagingRow(v, gtx) }
	awaitDrawn(t, e, "Loading more messages…")
	awaitDrawn(t, e, "Still loading… sorry for the long wait!")
	t.Cleanup(setViewsOnline(func(Host) bool { return false }))
	awaitDrawn(t, e, "Go online to load more threads.")
	awaitDrawn(t, e, "No connection.")
}

// The DegradedBanner in Threads and Unreads while the socket has trouble
// connecting, and its click: Threads reads its threads again, both ask
// client.counts (requestOfflineCounts).
func TestViewsDegradedBannersLikeSlack(t *testing.T) {
	since := time.Now()
	t.Cleanup(setViewsTrouble(func(Host) time.Time { return since }))
	check := func(name string, draw func(gtx layout.Context), banner func() (string, image.Rectangle), fs *fakeslack.Server, methods ...string) {
		t.Helper()
		e := &vaEnv{size: image.Pt(1000, 900), draw: draw}
		e.until(t, name+"'s banner", func() bool { txt, r := banner(); return strings.HasPrefix(txt, "Last updated") && !r.Empty() })
		before := map[string]int64{}
		for _, m := range methods {
			before[m] = fs.CallCount(m)
		}
		_, r := banner()
		e.click(r.Min.Add(r.Size().Div(2)))
		for _, m := range methods {
			e.until(t, name+"'s click asking "+m+" again", func() bool { return fs.CallCount(m) > before[m] })
		}
	}
	app, fs := vaFake(t)
	thr := NewThreads(&slickHost{app: app, th: theme.New(false)})
	check("Threads", func(gtx layout.Context) { thr.Layout(gtx) }, func() (string, image.Rectangle) { return threadsBanner(thr) }, fs,
		"subscriptions.thread.getView", "client.counts")
	app2, fs2 := vaFake(t)
	unr := NewUnreads(&slickHost{app: app2, th: theme.New(false)})
	check("Unreads", func(gtx layout.Context) { unr.Layout(gtx) }, func() (string, image.Rectangle) { return unreadsBanner(unr) }, fs2,
		"client.counts")
}

// Unreads' loading row (a channel's next page out) offline is
// InlineOfflineIndicator: "No connection." and "Go online to load more
// messages.".
func TestUnreadsLoadingRowOfflineLikeSlack(t *testing.T) {
	app, fs := vaFake(t)
	app.S.SetCounts(&slack.Counts{Channels: []slack.CountEntry{{ID: fakeslack.ViewsChannel, LastRead: "1762000000.000000",
		Latest: "1762700900.000100", HasUnreads: true, MentionCount: 0}}})
	fs.SetFault("conversations.history", fakeslack.Fault{Delay: 5 * time.Second})
	v := NewUnreads(&slickHost{app: app, th: theme.New(false)})
	e := &vaEnv{size: image.Pt(1000, 900)}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	awaitDrawn(t, e, "Loading more messages…")
	t.Cleanup(setViewsOnline(func(Host) bool { return false }))
	awaitDrawn(t, e, "Go online to load more messages.")
	awaitDrawn(t, e, "No connection.")
}
