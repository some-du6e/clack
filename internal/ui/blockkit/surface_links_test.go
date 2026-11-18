package blockkit

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// surfLinkHost records Slack's Link clicks (the ui root's FollowLink),
// which keeps permalinks and slack: links in the client.
type surfLinkHost struct {
	Host
	follows []string
}

func (h *surfLinkHost) FollowLink(u string, openRoot bool) bool {
	h.follows = append(h.follows, fmt.Sprintf("%s openRoot=%v", u, openRoot))
	return strings.HasPrefix(u, "slack://") || strings.Contains(u, ".slack.com/archives/")
}

// TestSurfaceLinksFollowSlackLinks: a link in a surface's text (a
// modal, an app's Home tab) is Slack's Link click: a permalink goes
// through openPermalink (a mrkdwn link's with openThreadForRootMessage,
// so a thread's parent opens its thread), a slack: link to
// handleDeepLink, anything else to the browser. Clack opened permalinks
// in the browser and dropped slack: links.
func TestSurfaceLinksFollowSlackLinks(t *testing.T) {
	var opened []string
	prev := openURL
	openURL = func(u string) { opened = append(opened, u) }
	t.Cleanup(func() { openURL = prev })
	h := &surfLinkHost{}
	s := &surfHandler{h: h}
	perma := "https://fakeclub.slack.com/archives/C0THPUBLIC1/p1762516000000100"
	deep := "slack://channel?team=T0FAKE&id=C0THPUBLIC1"
	other := "https://example.com/synthetic-docs"
	s.OpenURL(perma)
	s.OpenURL(deep)
	s.OpenURL(other)
	if m, ok := any(s).(interface{ OpenMrkdwnLink(url, text string) }); ok {
		m.OpenMrkdwnLink(perma, "the launch thread")
	} else {
		t.Errorf("a surface's mrkdwn link has no click of its own: a thread's parent wouldn't open its thread")
	}
	want := []string{perma + " openRoot=false", deep + " openRoot=false", other + " openRoot=false", perma + " openRoot=true"}
	if !slices.Equal(h.follows, want) {
		t.Errorf("Link clicks %q\nwant %q", h.follows, want)
	}
	if !slices.Equal(opened, []string{other}) {
		t.Errorf("opened in the browser: %q, want only the link outside Slack", opened)
	}
}
