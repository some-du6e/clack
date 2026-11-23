package popups

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/core"
)

// linkFollowHost is a Host with Slack's Link (the ui root's FollowLink),
// which takes this workspace's links and leaves another's.
type linkFollowHost struct {
	*tzHost
	follows []string
}

func (h *linkFollowHost) FollowLink(u string, openRoot bool) bool {
	h.follows = append(h.follows, fmt.Sprintf("%s openRoot=%v", u, openRoot))
	return strings.Contains(u, "fakeclub.slack.com/")
}

func catchBrowser(t *testing.T) *[]string {
	var opened []string
	prev := openBrowser
	openBrowser = func(u string) { opened = append(opened, u) }
	t.Cleanup(func() { openBrowser = prev })
	return &opened
}

// TestTopicLinksFollowSlackLinks: a link in the channel card's or the
// About page's topic is Slack's Link: in the client when it's one, with
// openThreadForRootMessage for the topic's mrkdwn links (BaseMrkdwnLink),
// else the browser. Clack opened every topic link in the browser.
func TestTopicLinksFollowSlackLinks(t *testing.T) {
	opened := catchBrowser(t)
	h := &linkFollowHost{tzHost: &tzHost{}}
	th := topicHandler{h: h}
	perma := "https://fakeclub.slack.com/archives/C0THPUBLIC1/p1762516000000100"
	other := "https://example.com/synthetic-runbook"
	th.OpenURL(perma)
	if m, ok := any(th).(interface{ OpenMrkdwnLink(url, text string) }); ok {
		m.OpenMrkdwnLink(perma, "the launch thread")
	} else {
		t.Errorf("a topic's mrkdwn link has no click of its own: a thread's parent wouldn't open its thread")
	}
	th.OpenURL(other)
	if want := []string{perma + " openRoot=false", perma + " openRoot=true", other + " openRoot=false"}; !slices.Equal(h.follows, want) {
		t.Errorf("Link clicks %q\nwant %q", h.follows, want)
	}
	if !slices.Equal(*opened, []string{other}) {
		t.Errorf("opened in the browser %q, want only the link outside Slack", *opened)
	}
}

// TestSearchCardForeignPermalinkOpensBrowser: a search result's forwarded
// message from another workspace is a permalink Slack's Link can't place
// (jumpToMessage finds no target workspace): openPermalink opens it in a
// new tab, and the conversation isn't a recent one. Clack navigated to a
// conversation it couldn't load, and recorded it in the recents.
func TestSearchCardForeignPermalinkOpensBrowser(t *testing.T) {
	srResetRecents()
	t.Cleanup(srResetRecents)
	opened := catchBrowser(t)
	h := &linkFollowHost{tzHost: &tzHost{}}
	card := &srAttCard{v: &SearchView{h: h}, m: core.Message{TS: "1762516000.000100", Channel: "C0THPUBLIC1"}}
	foreign := "https://otherco.slack.com/archives/C0FOREIGN01/p1762516060000100"
	card.open(srAttItem{kind: "message", link: foreign})
	if !slices.Equal(*opened, []string{foreign}) {
		t.Errorf("opened in the browser %q, want the foreign permalink", *opened)
	}
	if len(h.navs) > 0 {
		t.Errorf("the foreign permalink navigated to %q", h.navs)
	}
	srMu.Lock()
	recents := slices.Clone(srRecentConvs)
	srMu.Unlock()
	if slices.Contains(recents, "C0FOREIGN01") {
		t.Errorf("the foreign conversation is a recent one: %q", recents)
	}
}
