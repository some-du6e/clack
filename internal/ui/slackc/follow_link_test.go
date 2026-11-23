package slackc

import (
	"slices"
	"strings"
	"testing"
)

// TestPluginLinksFollowSlackLinks: an href a plugin's tree draws is
// Slack's Link (the ui root's FollowLink through Env): a Slack link opens
// in the client (a message, a conversation, a channel's tab), a slack:
// link goes to the app, and only a link FollowLink leaves reaches the
// browser (Env.OpenURL). Synthetic links.
func TestPluginLinksFollowSlackLinks(t *testing.T) {
	prev := env()
	t.Cleanup(func() { Init(prev) })
	var follows, opened, navs []string
	Init(Env{
		FollowLink: func(href string) bool {
			follows = append(follows, href)
			return strings.HasPrefix(href, "slack:") || strings.Contains(href, ".slack.com/archives/")
		},
		OpenURL:  func(u string) { opened = append(opened, u) },
		Navigate: func(r string) { navs = append(navs, r) },
	})
	tab := "https://fakeclub.slack.com/archives/C0THPUBLIC1/tabs/Ct0SYNTH01"
	perma := "https://fakeclub.slack.com/archives/C0THPUBLIC1/p1762516000000100"
	deep := "slack://channel?team=T0FAKE0001&id=C0THPUBLIC1"
	other := "https://example.com/synthetic"
	for _, u := range []string{tab, perma, deep, other} {
		followLink(u)
	}
	if want := []string{tab, perma, deep, other}; !slices.Equal(follows, want) {
		t.Errorf("Link clicks %q\nwant %q", follows, want)
	}
	if !slices.Equal(opened, []string{other}) || len(navs) > 0 {
		t.Errorf("opened in the browser %q, navigated %q; want only the link outside Slack in the browser", opened, navs)
	}
}
