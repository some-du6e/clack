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
	"fmt"
	"image"
	"os"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
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

func (e *perfEnv) channelImages(ch string) []messages.ImageRect {
	if c := e.a.convos[ch+"/"]; c != nil {
		if l, ok := c.list.(channelList); ok {
			return l.ImageRects()
		}
	}
	return nil
}

// clickUnfurlImage clicks the middle of image url of message ts in
// #embeds-gallery through the router and checks Slack's image viewer
// opened on it (recorded by opened) as the top overlay.
func (e *perfEnv) clickUnfurlImage(t *testing.T, opened func() []openedImage, ts, url string, clock *time.Duration) {
	t.Helper()
	var r image.Rectangle
	for _, ir := range e.channelImages(fakeslack.EmbedsChannelID) {
		if ir.TS == ts && ir.Image.URL == url {
			r = ir.R
		}
	}
	if r.Empty() {
		t.Fatalf("%s isn't on screen as a viewer image (%+v)", url, e.channelImages(fakeslack.EmbedsChannelID))
	}
	if r.Dx() < 300 || r.Dy() < 180 {
		t.Errorf("%s: image box %v, want the 360x225 image card", url, r)
	}
	before := len(opened())
	*clock += 500 * time.Millisecond
	e.clickAt(r.Min.Add(r.Max).Div(2), *clock)
	for k := 0; k < 4; k++ {
		e.frame()
	}
	got := opened()
	if len(got) != before+1 {
		t.Fatalf("clicking %s didn't open the image viewer", url)
	}
	if o := got[len(got)-1]; o.img.URL != url || o.ts != ts {
		t.Errorf("the viewer opened %s (message %s), want %s (%s)", o.img.URL, o.ts, url, ts)
	}
	if n := len(e.a.overlays); n == 0 {
		t.Errorf("no overlay after opening the viewer")
	} else if _, ok := e.a.overlays[n-1].(*popups.Lightbox); !ok {
		t.Errorf("top overlay is %T, not the image viewer", e.a.overlays[n-1])
	}
}

// Karim's case: a message linking two PNGs, each unfurled as an
// image-only attachment (from_url = original_url = image_url). Clicking
// either image opens it in Clack's image viewer (Slack's
// AttachmentImage onClick), not the browser.
func TestEmbedsImageViewer(t *testing.T) {
	opened := recordLightbox(t)
	e := embedsEnv(t, image.Pt(1440, 900), false)
	m := e.embedsMsg("image links")
	atts := docAtts(m)
	if len(atts) != 2 {
		t.Fatalf("image links: %d attachments", len(atts))
	}
	var clock time.Duration
	for _, a := range atts {
		if a.Image == nil || a.Image.W != 1440 || a.Image.H != 900 || a.OriginalURL != a.Image.URL || a.FromURL != a.Image.URL {
			t.Fatalf("image links attachment %+v", a)
		}
		e.clickUnfurlImage(t, opened, m.TS, a.Image.URL, &clock)
		e.a.dismissTop()
		for k := 0; k < 3; k++ {
			e.frame()
		}
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

			// the image viewer on an unfurl's image (a click on the
			// first image-only link of "image links"), then the second
			opened := recordLightbox(t)
			m := e.embedsMsg("image links")
			atts := docAtts(m)
			var clock time.Duration
			for i, a := range atts {
				e.settle(300 * time.Millisecond)
				e.clickUnfurlImage(t, opened, m.TS, a.Image.URL, &clock)
				e.settle(1500 * time.Millisecond) // the full-size image from the fake's /img
				e.embedsShot(fmt.Sprintf("embeds-viewer-%d-%s", i+1, mode))
				e.a.dismissTop()
			}
		})
	}
}
