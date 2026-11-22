package views

// The Threads view's card header for a thread in a canvas's or a list's
// file channel is Slack's InlineChannelEntity for it (getChannelIconType's
// file channel case, ChannelName's title or emoji), never a private
// channel's lock and the channel's raw name (canvas-mention round 6).

import (
	"net/url"
	"slices"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

func TestThreadsFileChannelCardsLikeSlack(t *testing.T) {
	const canvasRoot, listRoot = "1762700100.000100", "1762700200.000100"
	app, fs := vaFake(t)
	s := app.S
	s.CustomEmoji.Put(fakeslack.CanvasMentionEmoji, fs.CanvasMentionEmojiURL())
	s.PutConv(slack.Conversation{ID: fakeslack.FileChanLaunch, Name: "launch-plan", IsChannel: true, IsGroup: true,
		IsPrivate: true, IsFile: true, IsMember: true, NumMembers: 4})
	s.PutCanvasRef(fakeslack.CanvasChannel, richtext.CanvasRef{Title: "Launch plan", Status: "normal", Kind: "canvas"})
	s.PutConv(slack.Conversation{ID: fakeslack.FileChanKitchen, Name: ":" + fakeslack.CanvasMentionEmoji + ": Launch tracker",
		IsChannel: true, IsGroup: true, IsPrivate: true, IsFile: true, IsMember: true, NumMembers: 3})
	s.PutCanvasRef(fakeslack.ListKitchen, richtext.CanvasRef{Title: "Launch tracker", Status: "normal", Kind: "list"})
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
		if method == "subscriptions.thread.getView" {
			return thrOrderAnswer(
				thrOrderFix(fakeslack.FileChanLaunch, canvasRoot, "1762700150.000100", "U0FAKE00002", true),
				thrOrderFix(fakeslack.FileChanKitchen, listRoot, "1762700250.000100", "U0FAKE00002", true)), true
		}
		return nil, false
	})
	v := NewThreads(newKBHost(app, false))
	e := thrViewEnv(t, v, 2)
	e.frame()
	if c := v.cards[fakeslack.FileChanLaunch+"/"+canvasRoot]; c == nil || c.icon != "canvas" || c.name != "Launch plan" {
		t.Errorf("the canvas comment thread's card header %+v, want the canvas icon and its title", c)
	}
	c := v.cards[fakeslack.FileChanKitchen+"/"+listRoot]
	if c == nil || c.icon != "lists" ||
		!slices.ContainsFunc(c.spans, func(sp richtext.Span) bool { return sp.Kind == richtext.Emoji && sp.ID == fakeslack.CanvasMentionEmoji }) {
		t.Errorf("the list record thread's card header %+v, want the lists icon and the name's emoji", c)
	}
}
