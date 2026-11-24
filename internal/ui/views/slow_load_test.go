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
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func shortStillLoading(t *testing.T) {
	prev := messages.StillLoadingAfter
	messages.StillLoadingAfter = 300 * time.Millisecond
	t.Cleanup(func() { messages.StillLoadingAfter = prev })
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
	prev := viewsClientOnline
	viewsClientOnline = func(Host) bool { return false }
	t.Cleanup(func() { viewsClientOnline = prev })
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

	prev := viewsClientOnline
	viewsClientOnline = func(Host) bool { return false }
	t.Cleanup(func() { viewsClientOnline = prev })
	app2, _ := vaFake(t)
	v2 := NewUnreads(&slickHost{app: app2, th: theme.New(false)})
	e2 := &vaEnv{size: image.Pt(1000, 900)}
	e2.draw = func(gtx layout.Context) { v2.Layout(gtx) }
	awaitDrawn(t, e2, "Go online to get the latest messages.")
}
