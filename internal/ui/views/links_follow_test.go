package views

import (
	"fmt"
	"image"
	"slices"
	"sync"
	"testing"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// linkHost records a view's link clicks: Slack's Link (the ui root's
// FollowLink), the pill's jumpToMessage, and what reached the deep-link
// handler instead (OpenURL).
type linkHost struct {
	Host
	follows, urls, jumps []string
}

func (h *linkHost) FollowLink(u string, openRoot bool) bool {
	h.follows = append(h.follows, fmt.Sprintf("%s openRoot=%v", u, openRoot))
	return true
}
func (h *linkHost) OpenURL(u string) { h.urls = append(h.urls, u) }
func (h *linkHost) JumpToMessage(ch, ts, thread string) {
	h.jumps = append(h.jumps, ch+" "+ts+" "+thread)
}

// TestRowLinksFollowSlackLinks: a link in a view's row (Activity, saved
// items, files and the rest) is Slack's Link click: a permalink goes
// through openPermalink (a mrkdwn link's with openThreadForRootMessage),
// a slack: link to handleDeepLink, a message_mention pill to
// jumpToMessage with its own ids, and a file's page (a PDF in the Files
// view) is a link like any other. Clack handed every link to the
// deep-link handler, which opened nothing for a permalink, a help article
// or a file's page and logged its path.
func TestRowLinksFollowSlackLinks(t *testing.T) {
	h := &linkHost{}
	hh := hostHandler{h}
	const ch, root = "C0THPUBLIC1", "1762516000.000100"
	perma := "https://fakeclub.slack.com/archives/" + ch + "/p1762516000000100"
	deep := "slack://channel?team=T0FAKE&id=" + ch + "&message=1762516060.000100"
	hh.OpenURL(perma)
	hh.OpenURL(deep)
	pdf := "https://fakeclub.slack.com/files/U0FAKE00002/F0SYNTH0001/plan.pdf"
	openFile(h, fileItem{core.File{ID: "F0SYNTH0001", Filetype: "pdf", Permalink: pdf}})
	tab := "https://fakeclub.slack.com/archives/" + ch + "/tabs/Ct0SYNTH01" // a channel's Links tab row (LinkEntity)
	(&ChannelFiles{base: base{h: h}}).openLink(tab)
	if m, ok := any(hh).(interface{ OpenMrkdwnLink(url, text string) }); ok {
		m.OpenMrkdwnLink(perma, "the launch thread")
	} else {
		t.Errorf("a row's mrkdwn link has no click of its own: a thread's parent wouldn't open its thread")
	}
	if p, ok := any(hh).(interface {
		OpenMessageMention(ch, ts, thread, url string)
	}); ok {
		p.OpenMessageMention(ch, root, root, perma)
	} else {
		t.Errorf("a row's message_mention pill has no click of its own")
	}
	if want := []string{perma + " openRoot=false", deep + " openRoot=false", pdf + " openRoot=false", tab + " openRoot=false",
		perma + " openRoot=true"}; !slices.Equal(h.follows, want) {
		t.Errorf("Link clicks %q\nwant %q", h.follows, want)
	}
	if len(h.urls) > 0 {
		t.Errorf("links reached the deep-link handler: %q", h.urls)
	}
	if want := []string{ch + " " + root + " " + root}; !slices.Equal(h.jumps, want) {
		t.Errorf("the pill's jumps %q, want %q (its channel_id, message_ts and thread_ts)", h.jumps, want)
	}
}

// openAtHost records the navigations to a message whose thread is known
// (popups.MessageOpener).
type openAtHost struct {
	*vaHost
	mu    sync.Mutex
	opens []string
}

func (h *openAtHost) OpenAtMessage(ch, ts, thread string, highlightRoot bool) {
	h.mu.Lock()
	h.opens = append(h.opens, fmt.Sprintf("%s %s thread %s highlightRoot=%v", ch, ts, thread, highlightRoot))
	h.mu.Unlock()
}

// TestActivityOpenInHomeOpensAtReply: Activity's "Open in home" on a
// thread's item (WabB) is toThread({channelId, threadTs, replyTs: ts,
// highlightRoot}): the thread at the item's reply, highlighted, its root
// highlighted in the conversation. Clack opened the thread at its top,
// the reply neither placed nor highlighted.
func TestActivityOpenInHomeOpensAtReply(t *testing.T) {
	log := &callLog{}
	app := actFeedAppWith(t, log.rec)
	h := &openAtHost{vaHost: &vaHost{slickHost: &slickHost{app: app, th: theme.New(false)}}}
	v := NewActivity(h)
	e := &vaEnv{size: image.Pt(1200, 900)}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	e.until(t, "the activity feed", func() bool { return len(actPage(v).Items) >= actFeedLen })
	e.frame()
	it := actItem(t, v, "k-thread-many")
	thread := actThread(it)
	if thread == "" || it.TS == "" || it.TS == thread {
		t.Fatalf("k-thread-many: ts %q, thread %q; want a reply in a thread", it.TS, thread)
	}
	var open func()
	for _, mi := range v.itemMenu(h, *it, actKey(it), false) {
		if mi.Key == "open_in_home" {
			open = mi.Do
		}
	}
	if open == nil {
		t.Fatal("the item's menu has no Open in home")
	}
	open()
	h.mu.Lock()
	got := slices.Clone(h.opens)
	h.mu.Unlock()
	if want := []string{it.Channel + " " + it.TS + " thread " + thread + " highlightRoot=true"}; !slices.Equal(got, want) {
		t.Errorf("Open in home opened %q, want %q (toThread at the reply, highlightRoot)", got, want)
	}
}
