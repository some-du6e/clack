package ui

// Link previews against internal/fakeslack's #embeds-gallery (synthetic
// only; nothing talks to Slack): an X post, a YouTube video, a GitHub app
// unfurl, a site with a small thumb, one with a large image, a text-only
// site, an image link and a Block Kit unfurl (docs/EMBEDS.md). The render
// test checks the docs and removes the viewer's X preview through
// core.DeleteAttachment (the store drops it, chat.deleteAttachment goes
// out); with EMBEDS_SHOTS_DIR set (the embeds-shots workflow) it saves
// the whole channel and the "Remove preview?" dialog, light and dark.

import (
	"context"
	"image"
	"os"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

func embedsEnv(t *testing.T, size image.Point, dark bool) *perfEnv {
	t.Helper()
	sz := fakeslack.Small
	sz.Embeds = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.size = size
	if os.Getenv("EMBEDS_SHOTS_DIR") != "" {
		if e.gpu != nil {
			e.gpu.Release()
		}
		w, err := pixeltest.NewWindow(size.X, size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	if dark {
		e.a.setDark(true)
	}
	e.openChannel(fakeslack.EmbedsChannelID)
	n := len(fs.EmbedsCases())
	e.until("the #embeds-gallery history", 20*time.Second, func() bool { return e.loadedMessages(fakeslack.EmbedsChannelID) >= n })
	e.settle(1500 * time.Millisecond) // images from the fake's /img
	return e
}

func (e *perfEnv) embedsShot(name string) {
	if os.Getenv("EMBEDS_SHOTS_DIR") == "" {
		return
	}
	t, ok := e.tb.(*testing.T)
	if !ok {
		return
	}
	e.settle(800 * time.Millisecond)
	e.shotTo(t, os.Getenv("EMBEDS_SHOTS_DIR"), name)
}

func (e *perfEnv) embedsMsg(name string) core.Message {
	e.tb.Helper()
	c := e.a.data.core
	for _, ec := range e.fs.EmbedsCases() {
		if ec.Name != name {
			continue
		}
		m, ok := c.S.Find(fakeslack.EmbedsChannelID, ec.TS)
		if !ok {
			e.tb.Fatalf("%s (%s) isn't loaded", name, ec.TS)
		}
		return c.MessageFromSlack(fakeslack.EmbedsChannelID, &m)
	}
	e.tb.Fatalf("no embeds case %q", name)
	return core.Message{}
}

func docAtts(m core.Message) []*richtext.Attachment {
	var out []*richtext.Attachment
	for _, b := range m.Doc.Blocks {
		if b.Kind == richtext.AttachmentBlock && b.Att != nil {
			out = append(out, b.Att)
		}
	}
	return out
}

func TestEmbedsChannel(t *testing.T) {
	e := embedsEnv(t, image.Pt(1440, 900), false)
	x := docAtts(e.embedsMsg("x post"))
	if len(x) != 1 || x[0].ID != "1" || x[0].Image == nil || x[0].Image.W != 1200 || x[0].VideoHTML || !x[0].LinkedInText {
		t.Fatalf("x post attachment %+v", x)
	}
	if yt := docAtts(e.embedsMsg("youtube")); len(yt) != 1 || !yt[0].VideoHTML || yt[0].Thumb == nil || yt[0].Thumb.W != 480 {
		t.Errorf("youtube attachment %+v", yt)
	}
	if gh := docAtts(e.embedsMsg("github")); len(gh) != 1 || gh[0].BotID == "" || gh[0].Color == "" {
		t.Errorf("github attachment %+v", gh)
	}
	if kit := docAtts(e.embedsMsg("block kit")); len(kit) != 1 || !kit[0].Kit {
		t.Errorf("block kit attachment %+v", kit)
	}
	for i := 0; i < 3; i++ {
		e.frame()
	}
	// remove the viewer's X preview the way the dialog does
	ts := ""
	for _, ec := range e.fs.EmbedsCases() {
		if ec.Name == "x post" {
			ts = ec.TS
		}
	}
	if err := e.a.data.core.DeleteAttachment(context.Background(), fakeslack.EmbedsChannelID, ts, "1"); err != nil {
		t.Fatal(err)
	}
	if got := docAtts(e.embedsMsg("x post")); len(got) != 0 {
		t.Errorf("after removing: %d attachments", len(got))
	}
	if d := e.fs.DeletedAttachments(); len(d) != 1 || d[0].TS != ts || d[0].Attachment != "1" || d[0].Channel != fakeslack.EmbedsChannelID {
		t.Errorf("chat.deleteAttachment calls %+v", d)
	}
	// someone else's preview: Slack refuses
	yts := ""
	for _, ec := range e.fs.EmbedsCases() {
		if ec.Name == "youtube" {
			yts = ec.TS
		}
	}
	if err := e.a.data.core.DeleteAttachment(context.Background(), fakeslack.EmbedsChannelID, yts, "1"); err == nil {
		t.Error("removing another person's preview succeeded")
	}
	for i := 0; i < 3; i++ {
		e.frame()
	}
}

func TestEmbedsShots(t *testing.T) {
	if os.Getenv("EMBEDS_SHOTS_DIR") == "" {
		t.Skip("EMBEDS_SHOTS_DIR not set (the embeds-shots workflow sets it)")
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			tall := embedsEnv(t, image.Pt(1440, 3000), dark)
			tall.embedsShot("embeds-channel-" + mode)
			tall.shutdown()

			e := embedsEnv(t, image.Pt(1440, 900), dark)
			e.embedsShot("embeds-bottom-" + mode)
			x := e.embedsMsg("x post")
			popups.ConfirmRemovePreview(e.a, x.Channel, x.TS, "1")
			e.embedsShot("embeds-remove-dialog-" + mode)
			e.a.dismissTop()
		})
	}
}
