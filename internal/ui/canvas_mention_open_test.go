package ui

// Canvas mentions open the canvas, never the canvas's hidden file channel
// (Karim, α.102: a click "put me in a limbo esque channel where the canvas
// contents are messages??"), and canvas titles draw their emoji (the
// channel's canvas tab read "clack testing :cat-slackbot:"). Slack's
// jumpToMessage opens a canvas's file channel message as the canvas at the
// mention's section; its tab label renders title_blocks from the emoji
// store each time. internal/fakeslack, made-up content only.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/canvasview"
)

// cmSeed gives the shell's store what core has once the mention's message
// arrived: the file channel known as the private conversation it is, the
// canvas's files.info, the emoji.
func cmSeed(e *actShellEnv) {
	e.fs.AddCanvasMention()
	s := e.ca.S
	s.PutConv(slack.Conversation{ID: fakeslack.CanvasMentionChannel, Name: fakeslack.CanvasMentionTitle, IsChannel: true, IsGroup: true,
		IsPrivate: true, IsFile: true, IsMember: true, NumMembers: 2})
	blocks, _ := json.Marshal(fakeslack.CanvasMentionTitleBlocks())
	s.PutCanvasRef(fakeslack.CanvasMentionFile, richtext.CanvasRef{Title: fakeslack.CanvasMentionTitle, TitleBlocks: blocks,
		Status: "normal", Kind: "canvas"})
	s.CustomEmoji.Put(fakeslack.CanvasMentionEmoji, e.fs.CanvasMentionEmojiURL())
	e.a.data.fetched[fakeslack.CanvasMentionChannel] = time.Now() // no header reads for it
}

// The inbox's click on a canvas mention shows the canvas viewer on that
// canvas in the column beside the inbox, not the file channel's messages.
func TestActivityCanvasMentionOpensCanvas(t *testing.T) {
	e := newActShell(t)
	cmSeed(e)
	e.a.Navigate("activity")
	e.until("the activity view", func() bool { return e.activity() != nil })
	e.until("the activity feed", func() bool { return e.fs.CallCount("activity.feed") > 0 })
	e.frames(20)
	e.activity().StepItem(1, false) // the newest row: the canvas mention
	e.frames(10)
	if k := e.activity().OpenedItem(); k != fakeslack.CanvasMentionKey {
		t.Fatalf("opened %q, want the canvas mention %s (the newest row)", k, fakeslack.CanvasMentionKey)
	}
	if e.a.secondary.kind == secChannel && e.a.secondary.channel == fakeslack.CanvasMentionChannel {
		t.Fatalf("the column opened the canvas's file channel %s as a channel", fakeslack.CanvasMentionChannel)
	}
	v, ok := e.a.secondary.view.(*canvasview.View)
	if !e.a.secondary.open() || !ok || v.ID() != fakeslack.CanvasMentionFile {
		t.Fatalf("the column shows %v (%T), want the canvas viewer on %s", e.a.secondary.kind, e.a.secondary.view, fakeslack.CanvasMentionFile)
	}
	if r := string(e.a.router.Current()); strings.Contains(r, fakeslack.CanvasMentionChannel) {
		t.Errorf("the route is the file channel's: %s", r)
	}
}

// A jump to the mention's message (a link, a search result, a
// notification: c/<file channel>/m/<ts>) opens the canvas in the full
// view; the file channel's route never shows.
func TestCanvasMentionMessageJumpOpensCanvas(t *testing.T) {
	e := newActShell(t)
	cmSeed(e)
	e.a.Navigate("activity")
	e.frames(5)
	e.a.Navigate("c/" + fakeslack.CanvasMentionChannel + "/m/" + fakeslack.CanvasMentionTS)
	e.frames(5)
	if r := string(e.a.router.Current()); r != "docs/"+fakeslack.CanvasMentionFile {
		t.Errorf("the jump went to %q, want the canvas's full view docs/%s", r, fakeslack.CanvasMentionFile)
	}
	// the toast's click: jumpToMessage for the mention
	e.a.Navigate("activity")
	e.frames(3)
	e.a.notificationClicked(fakeslack.CanvasMentionChannel, fakeslack.CanvasMentionTS, "", "")
	e.frames(5)
	if r := string(e.a.router.Current()); r != "docs/"+fakeslack.CanvasMentionFile {
		t.Errorf("the notification's click went to %q, want docs/%s", r, fakeslack.CanvasMentionFile)
	}
}

// A canvas tab whose title emoji the emoji map didn't have when the tab
// bar was read (a warm start's map still loading, an emoji newer than the
// cached map) draws it as an image once the map has it: Karim's channel
// tab read "clack testing :cat-slackbot:".
func TestCanvasTabTitleEmojiLate(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	const ch, file, name = "C0FAKE00001", "F0TABEMOJ01", "late-emoji-fixture"
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	fs.AddCanvas(fakeslack.FakeCanvas{ID: file, Channel: ch, Title: "clack testing :" + name + ":",
		TitleBlocks: []any{map[string]any{"type": "rich_text", "block_id": "t", "elements": []any{
			map[string]any{"type": "rich_text_section", "elements": []any{
				map[string]any{"type": "text", "text": "clack testing "},
				map[string]any{"type": "emoji", "name": name}}}}}}})
	e.openChannel(ch)
	var spans []richtext.Span
	e.until("the canvas tab", 15*time.Second, func() bool {
		if !e.a.data.tabsDone[ch] {
			return false
		}
		for _, tb := range e.a.channelTabs(ch) {
			if tb.fileID == file {
				spans = tb.spans
				return true
			}
		}
		return false
	})
	for _, s := range spans {
		if s.Kind == richtext.Emoji && s.URL != "" {
			t.Fatalf("precondition: the emoji map had %s already (%+v)", name, spans)
		}
	}
	url := fs.URL + "img/64x64/" + fakeslack.TeamID + "/" + name + "/00000000000000ee.png"
	e.a.Core().S.CustomEmoji.Put(name, url) // emoji_changed, or the map's refresh
	e.until("the tab title's emoji as an image", 15*time.Second, func() bool {
		for _, tb := range e.a.channelTabs(ch) {
			if tb.fileID != file {
				continue
			}
			for _, s := range tb.spans {
				if s.Kind == richtext.Emoji && s.URL == url {
					return true
				}
			}
			spans = tb.spans
		}
		return false
	})
	for _, tb := range e.a.channelTabs(ch) {
		if tb.fileID == file && strings.Contains(richtext.PlainSpans(tb.spans), "clack testing") == false {
			t.Errorf("the tab's title lost its text: %+v", tb.spans)
		}
	}
	e.frame()
}
