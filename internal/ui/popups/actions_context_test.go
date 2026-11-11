package popups

import (
	"image"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func ctxLabels(m *Menu) string {
	var out []string
	for _, it := range m.Items() {
		switch {
		case it.Separator:
			out = append(out, "-")
		case len(it.Sub) > 0:
			var sub []string
			for _, s := range it.Sub {
				sub = append(sub, s.Label)
			}
			out = append(out, it.Label+"["+strings.Join(sub, ",")+"]")
		default:
			out = append(out, it.Label)
		}
	}
	return strings.Join(out, "|")
}

// The menus' rows are Slack's (popups/actions_context.go has the sources).
func TestContextMenuShapes(t *testing.T) {
	h := &tzHost{th: theme.New(false)}
	at := image.Pt(100, 100)
	if got := ctxLabels(NewLinkMenu(h, at, LinkMenu{URL: "https://example.com/a"})); got != "Copy Link|Open Link" {
		t.Errorf("link menu %q", got)
	}
	if got := ctxLabels(NewLinkMenu(h, at, LinkMenu{URL: "https://example.com/a", Image: "https://example.com/e.png"})); got != "Copy Link|Open Link|-|Copy Image|Copy Image URL" {
		t.Errorf("link with image %q", got)
	}
	mail := NewLinkMenu(h, at, LinkMenu{URL: "mailto:alice%40example.com?subject=hi"})
	if got := ctxLabels(mail); got != "Copy Email Address|Open Link" {
		t.Errorf("mailto menu %q", got)
	}
	if got := ctxLabels(NewImageMenu(h, at, "https://example.com/e.png", false)); got != "Copy Image|Copy Image URL" {
		t.Errorf("image menu %q", got)
	}
	m := core.Message{Channel: "C1", TS: "1700000000.000100"}
	img := &richtext.File{ID: "F1", Name: "a.png", IsImage: true, URL: "https://files.slack.com/a.png", Permalink: "https://x.slack.com/files/U/F1/a.png"}
	if got := ctxLabels(NewFileMenu(h, at, m, img)); got != "Download|-|Share file…|Copy link|Copy image|-|Open in[Open in browser,Open in new tab]" {
		t.Errorf("image file menu %q", got)
	}
	vid := &richtext.File{ID: "F2", Name: "a.mp4", IsVideo: true, URL: "https://files.slack.com/a.mp4", Permalink: "https://x.slack.com/files/U/F2/a.mp4"}
	if got := ctxLabels(NewFileMenu(h, at, m, vid)); got != "Download|Share file…|Copy link|-|Open in[Open in browser,Open in thread,Open in new window]" {
		t.Errorf("video file menu %q", got)
	}
	if got := ctxLabels(NewTextSelectionMenu(h, at, "hello")); got != "Copy text|Search in Slack" {
		t.Errorf("selection menu %q", got)
	}
	if got := ctxSearchQuery("  two\nlines\r\nhere "); got != "two lines here" {
		t.Errorf("search query %q", got)
	}
}

// In the thread pane the context menu has no thread action; in the
// Threads view it's "View thread"; on a message with replies "Reply to
// thread". Without a team allowing private_message_forwarding (no core
// here) the share row says "Share message…", like the toolbar (6bWJ).
func TestMessageContextMenuThreadAction(t *testing.T) {
	h := &tzHost{th: theme.New(false)}
	at := image.Pt(100, 100)
	m := core.Message{Channel: "C1", TS: "1700000000.000100", User: "UOTHER"}
	first := func(where MessageContext, m core.Message) string {
		var out []string
		for _, it := range NewMessageContextMenu(h, at, m, where, MessageMenuOpts{}).Items() {
			if it.Separator {
				break
			}
			out = append(out, it.Label)
		}
		return strings.Join(out, "|")
	}
	if got := first(InChannel, m); got != "Add reaction…|Reply in thread|Share message…|Add to-do" {
		t.Errorf("channel %q", got)
	}
	if got := first(InThread, m); got != "Add reaction…|Share message…|Add to-do" {
		t.Errorf("thread pane %q", got)
	}
	if got := first(InThreadsView, m); got != "Add reaction…|View thread|Share message…|Add to-do" {
		t.Errorf("threads view %q", got)
	}
	r := m
	r.ReplyCount, r.Saved = 2, true
	if got := first(InChannel, r); got != "Add reaction…|Reply to thread|Share message…|Complete to-do" {
		t.Errorf("with replies, saved %q", got)
	}
}
