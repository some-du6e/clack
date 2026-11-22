package ui

// Slack's Link click (boot-shared jRB6) through the real App, for the
// surfaces that hand their links to App.FollowLink (views' rows, the topic
// card, canvases, slackc, search cards, Block Kit surfaces, toasts):
// openPermalink opens every kind of Slack link it parses in the client
// (0S61 openPermalinkInClient: a channel's tab, a canvas, a list, a
// message or a conversation), a slack: link goes to the app's deep-link
// handler (slack://open and slack://room too), and only a link Slack
// doesn't open itself reaches the browser. The surfaces' own tests check
// that each hands its links to FollowLink; these check what FollowLink
// does with them. Synthetic fakeslack fixtures.

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/canvasview"
	"github.com/some-du6e/clack/internal/ui/messages"
)

// linkEnv is a workspace with the Files & links tab fixture and the thread
// header fixtures.
func linkEnv(t *testing.T) *perfEnv {
	t.Helper()
	sz := fakeslack.Small
	sz.Files, sz.ThreadHeader = true, true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	return e
}

// browserLog catches what reaches the browser: the ui root's openURL and
// a message's (messages.SwapOpenURL).
type browserLog struct {
	mu     sync.Mutex
	opened []string
}

func catchBrowser(t *testing.T) *browserLog {
	b := &browserLog{}
	rec := func(u string) {
		b.mu.Lock()
		b.opened = append(b.opened, u)
		b.mu.Unlock()
	}
	prev := openURL
	openURL = rec
	t.Cleanup(func() { openURL = prev })
	t.Cleanup(messages.SwapOpenURL(rec))
	return b
}

func (b *browserLog) got() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.opened)
}

// tabLink is Slack's getChannelTabPermalink for the Files & links tab.
func tabLink() string {
	return "https://fakeclub.slack.com/archives/" + fakeslack.FilesChannelID + "/tabs/" + fakeslack.FilesTabID
}

// TestFollowLinkOpensSlackLinksInClient: a channel tab's permalink opens
// the tab, a canvas's link the canvas, slack://open Home (window.open
// hands it back to the app), and a link outside Slack is the caller's to
// open in the browser. Clack's FollowLink parsed only messages and
// conversations, so a tab's or a canvas's link went to the browser and
// slack://open did nothing.
func TestFollowLinkOpensSlackLinksInClient(t *testing.T) {
	t.Run("a channel's tab", func(t *testing.T) {
		e := linkEnv(t)
		b := catchBrowser(t)
		e.openChannel(fakeslack.ThreadPrivateID)
		e.settle(300 * time.Millisecond)
		if !e.a.FollowLink(tabLink(), false) {
			t.Fatalf("the tab's permalink was left to the browser")
		}
		e.until("the Files & links tab", 15*time.Second, func() bool { return e.a.filesTabView(fakeslack.FilesChannelID) != nil })
		if r := e.a.router.Current(); r.Channel() != fakeslack.FilesChannelID {
			t.Errorf("route %q, want the tab's channel", r)
		}
		if got := b.got(); len(got) > 0 {
			t.Errorf("the browser opened %q", got)
		}
	})
	t.Run("a canvas", func(t *testing.T) {
		e := linkEnv(t)
		b := catchBrowser(t)
		e.openChannel(fakeslack.ThreadPrivateID)
		e.settle(300 * time.Millisecond)
		const id = "F0SYNTHCNV1"
		if !e.a.FollowLink("https://fakeclub.slack.com/docs/"+fakeslack.TeamID+"/"+id, false) {
			t.Fatalf("the canvas's link was left to the browser")
		}
		e.frame()
		if v, ok := e.a.secondary.view.(*canvasview.View); !ok || v.ID() != id {
			t.Errorf("the canvas %s didn't open beside the conversation: %s", id, e.openState())
		}
		if got := b.got(); len(got) > 0 {
			t.Errorf("the browser opened %q", got)
		}
	})
	t.Run("slack://open", func(t *testing.T) {
		e := linkEnv(t)
		e.openChannel(fakeslack.ThreadPrivateID)
		e.settle(300 * time.Millisecond)
		if !e.a.FollowLink("slack://open", false) {
			t.Fatalf("slack://open was left to the browser")
		}
		e.until("Home", 10*time.Second, func() bool { return e.a.router.Current() == Route("home") })
	})
	t.Run("a link outside Slack", func(t *testing.T) {
		e := linkEnv(t)
		e.openChannel(fakeslack.ThreadPrivateID)
		e.settle(300 * time.Millisecond)
		if e.a.FollowLink("https://example.com/synthetic-runbook", false) {
			t.Errorf("a link outside Slack was taken in the client")
		}
		if r := e.a.router.Current(); r != Route("c/"+fakeslack.ThreadPrivateID) {
			t.Errorf("route %q after a link outside Slack", r)
		}
	})
}

// TestTopicCardLinksFollowSlackLinks: the topic card's links are Slack's
// Link through the real App: a channel tab's permalink opens the tab, a
// link to a thread's parent in the topic's mrkdwn opens its thread
// (BaseMrkdwnLink's openThreadForRootMessage), and only a link outside
// Slack reaches the browser. Clack sent the tab's permalink to the
// browser and opened the parent's conversation without its thread.
func TestTopicCardLinksFollowSlackLinks(t *testing.T) {
	e := linkEnv(t)
	b := catchBrowser(t)
	pub, root := fakeslack.ThreadPublicID, fakeslack.ThreadRootTS
	e.openChannel(fakeslack.ThreadPrivateID)
	e.settle(300 * time.Millisecond)
	h := cardHandler{e.a}
	h.OpenURL(tabLink())
	e.until("the Files & links tab", 15*time.Second, func() bool { return e.a.filesTabView(fakeslack.FilesChannelID) != nil })
	if got := b.got(); len(got) > 0 {
		t.Errorf("the tab's permalink reached the browser: %q", got)
	}
	m, ok := any(h).(interface{ OpenMrkdwnLink(url, text string) })
	if !ok {
		t.Fatalf("the topic card's mrkdwn links have no click of their own (openThreadForRootMessage)")
	}
	m.OpenMrkdwnLink(digestLink(pub, root, ""), "the launch thread")
	e.waitDrawn(t, "the parent's thread", 20*time.Second, "", pub, root, func(rows []messages.SenderView) bool { return threadDrawn(rows, root) })
	h.OpenURL("https://example.com/synthetic-runbook")
	if got := b.got(); !slices.Equal(got, []string{"https://example.com/synthetic-runbook"}) {
		t.Errorf("the browser opened %q, want only the link outside Slack", got)
	}
}
