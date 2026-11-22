package canvasview

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// followHost records the canvas's link clicks that reach Slack's Link
// (the ui root's FollowLink), which takes the Slack links.
type followHost struct {
	Host
	follows, navs []string
}

func (h *followHost) FollowLink(u string, openRoot bool) bool {
	h.follows = append(h.follows, fmt.Sprintf("%s openRoot=%v", u, openRoot))
	return strings.Contains(u, ".slack.com/archives/")
}
func (h *followHost) Navigate(r string) { h.navs = append(h.navs, r) }

// TestCanvasLinksFollowSlackLinks: a link in a canvas is Slack's Link: a
// permalink (to a message, a conversation or one of its tabs) goes to the
// ui root's FollowLink, which opens it in the client, without
// openThreadForRootMessage (a canvas draws no BaseMrkdwnLink); a link to
// another canvas opens it; only a link FollowLink leaves goes to the
// browser. Synthetic links.
func TestCanvasLinksFollowSlackLinks(t *testing.T) {
	h := &followHost{}
	var canvases, opened []string
	v := &View{h: h, id: "F0SYNTHCNV1", act: Actions{
		Canvas:  func(id string) { canvases = append(canvases, id) },
		OpenURL: func(u string) { opened = append(opened, u) },
	}}
	r := &handler{v: v}
	perma := "https://fakeclub.slack.com/archives/C0THPUBLIC1/p1762516000000100"
	tab := "https://fakeclub.slack.com/archives/C0THPUBLIC1/tabs/Ct0SYNTH01"
	other := "https://example.com/synthetic"
	for _, u := range []string{perma, tab, "https://fakeclub.slack.com/docs/T0FAKE0001/F0SYNTHCNV2", other} {
		r.OpenURL(u)
	}
	if want := []string{perma + " openRoot=false", tab + " openRoot=false", other + " openRoot=false"}; !slices.Equal(h.follows, want) {
		t.Errorf("Link clicks %q\nwant %q", h.follows, want)
	}
	if !slices.Equal(canvases, []string{"F0SYNTHCNV2"}) {
		t.Errorf("canvases opened %q, want the linked one", canvases)
	}
	if !slices.Equal(opened, []string{other}) || len(h.navs) > 0 {
		t.Errorf("opened in the browser %q, navigated %q; want only the link outside Slack in the browser", opened, h.navs)
	}
}
