package ui

// The Threads page loads afresh when its sidebar row is clicked while it
// shows and when it comes back on screen (views.Threads' Reselect and
// PageLeft, from sbLinkClicked and afterRoute). Slack's row dispatches
// incrementAllThreadsRefreshCounter on a click while selected
// (AllThreadsRowElement), which ThreadsView answers with reloadThreads, and
// the view mounts again on the way back (componentDidMount's loadThreads).
// The page is cached across visits here, so nothing reloaded it.

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/views"
)

func TestThreadsPageReloadsLikeSlack(t *testing.T) {
	sz := fakeslack.Small
	sz.Views = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	getViews := func() int64 { return fs.CallCount("subscriptions.thread.getView") }
	// a click while the view's load is out does nothing (Slack's
	// this.loading), so each step waits for the view to settle
	settled := func() bool {
		v, ok := e.a.views[Route("threads")].(*views.Threads)
		return ok && v.Settled()
	}
	e.a.Navigate("threads")
	e.until("the Threads page's load", 15*time.Second, func() bool { return getViews() > 0 && settled() })
	n := getViews()
	for _, step := range []struct {
		what  string
		route string
		want  int64
	}{
		{"its row clicked again", "threads", n + 1},
		{"another page", "huddles", n + 1},
		{"back on it", "threads", n + 2},
	} {
		e.a.sbLinkClicked(step.route)
		e.until(step.what, 15*time.Second, func() bool { return getViews() >= step.want && settled() })
		e.settle(300 * time.Millisecond)
		if got := getViews(); got != step.want {
			t.Errorf("%s: %d getView calls, want %d", step.what, got, step.want)
		}
	}
}
