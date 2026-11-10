package richview

import (
	"encoding/json"
	"image"
	"strconv"
	"testing"
	"time"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/fakeslack"
	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// What a click on each attachment image does, as in Slack's web client:
// every image_url drawn by AttachmentMedia (AttachmentImage, NANx) opens
// the image viewer: the image card of a bare image link, the X/Bluesky,
// Flickr, video and music cards' media, the row card's right box, the
// classic (v1) card's image, and a Block Kit image block in an unfurl.
// AttachmentImage's onClick calls preventDefault, so the v3 card's <a>
// around it doesn't navigate. A thumb_url alone is AttachmentThumb, a
// span with no handler: in a v3 card the click is the card's link, in a
// v1 card (not a link) nothing. video_html's poster opens the link
// (Slack plays the iframe inline; Clack has no player).

const clickBase = "http://img.test/"

func clickImg(w, h int, seed string) string {
	return clickBase + "img/" + strconv.Itoa(w) + "x" + strconv.Itoa(h) + "/" + seed + ".png"
}

func clickDoc(t *testing.T, name string) rt.Doc {
	t.Helper()
	for _, f := range fakeslack.EmbedsFixtures(clickBase) {
		if f.Name == name {
			m := slack.Message{Type: "message", TS: "1762700000.000200", User: "U0", Text: f.Text, Attachments: f.Attachments}
			return rt.FromMessage(codeRes{}, &m)
		}
	}
	t.Fatalf("no embeds fixture %q", name)
	return rt.Doc{}
}

func attDoc(text string, atts ...slack.Attachment) rt.Doc {
	m := slack.Message{Type: "message", TS: "1762700000.000200", User: "U0", Text: text, Attachments: atts}
	return rt.FromMessage(codeRes{}, &m)
}

func clickHarness(d rt.Doc) (*harness, *recHandler) {
	h := &recHandler{}
	hs := &harness{st: newTestStyle(), v: New(d, Options{}), h: h, w: 800}
	hs.frame()
	return hs, h
}

func (hs *harness) clickAt(p image.Point, at *time.Duration) {
	*at += time.Second
	q := f32.Pt(float32(p.X), float32(p.Y))
	hs.send(press(q, *at), release(q, *at+20*time.Millisecond))
}

// imgPrimAt is the box of the drawn image with url.
func imgPrimAt(t *testing.T, v *View, url string) image.Rectangle {
	t.Helper()
	for _, im := range v.d.imgs {
		if im.url == url {
			return im.r
		}
	}
	t.Fatalf("no image %s drawn", url)
	return image.Rectangle{}
}

func mid(r image.Rectangle) image.Point { return r.Min.Add(r.Max).Div(2) }

func TestAttachmentImageClicks(t *testing.T) {
	const site = "https://site.example.com/post"
	type viewerCase struct {
		name string
		doc  rt.Doc
		kind attKind // of the first attachment (akOld: not checked)
		urls []string
	}
	viewer := []viewerCase{
		{"image links (image card)", clickDoc(t, "image links"), akImageCard,
			[]string{clickImg(1440, 900, "shot-a"), clickImg(1440, 900, "shot-b")}},
		{"image link (image card)", clickDoc(t, "image link"), akImageCard, []string{clickImg(800, 600, "pic-only")}},
		{"x post (social card)", clickDoc(t, "x post"), akSocial, []string{clickImg(1200, 675, "x-post")}},
		{"large image (row card)", clickDoc(t, "large image"), akGeneric, []string{clickImg(1200, 630, "blog-hero")}},
		{"flickr card", attDoc("<"+site+">", slack.Attachment{ID: 1, ServiceName: "Flickr", Title: "Sunset", TitleLink: site,
			FromURL: site, OriginalURL: site, ImageURL: clickImg(1024, 768, "flickr"), ImageWidth: 1024, ImageHeight: 768}),
			akFlickr, []string{clickImg(1024, 768, "flickr")}},
		{"music card", attDoc("<"+site+">", slack.Attachment{ID: 1, ServiceName: "Spotify", Title: "A song", TitleLink: site,
			FromURL: site, OriginalURL: site, ImageURL: clickImg(640, 640, "album"), ImageWidth: 640, ImageHeight: 640}),
			akMusic, []string{clickImg(640, 640, "album")}},
		{"classic card image_url", attDoc("bot post", slack.Attachment{ID: 1, Color: "#36a64f", BotID: "B0FAKEBOT1", Title: "Build 12",
			TitleLink: site, Text: "passed", ImageURL: clickImg(600, 300, "chart"), ImageWidth: 600, ImageHeight: 300}),
			akV1, []string{clickImg(600, 300, "chart")}},
		{"block kit image in an unfurl", attDoc("<"+site+">", slack.Attachment{ID: 1, FromURL: site, IsAppUnfurl: true,
			Blocks: json.RawMessage(`[{"type":"image","block_id":"i1","image_url":"` + clickImg(500, 250, "kit-img") + `","alt_text":"diagram"}]`)}),
			akV2, []string{clickImg(500, 250, "kit-img")}},
	}
	for _, c := range viewer {
		hs, h := clickHarness(c.doc)
		if c.kind != akOld {
			if k := kindOf(attBlock(t, c.doc)); k != c.kind {
				t.Errorf("%s: kind %d, want %d", c.name, k, c.kind)
			}
		}
		rs := hs.v.ImageRects()
		if len(rs) != len(c.urls) {
			t.Errorf("%s: %d viewer images, want %d", c.name, len(rs), len(c.urls))
			continue
		}
		var at time.Duration
		for i, r := range rs {
			if r.Image.URL != c.urls[i] {
				t.Errorf("%s: image %d is %s, want %s", c.name, i, r.Image.URL, c.urls[i])
			}
			// the image's middle and a point near its corner
			for _, p := range []image.Point{mid(r.R), r.R.Min.Add(image.Pt(6, 6))} {
				n := len(h.images)
				hs.clickAt(p, &at)
				if len(h.images) != n+1 {
					t.Errorf("%s: clicking image %d at %v didn't open the viewer (urls %q)", c.name, i, p, h.urls)
					continue
				}
				if got := h.images[len(h.images)-1].URL; got != c.urls[i] {
					t.Errorf("%s: the viewer opened %s, want %s", c.name, got, c.urls[i])
				}
			}
			// a right click on a v3 card's image is the card's (the
			// message menu here), not the image menu
			if hit := hs.v.HitAt(f32.Pt(float32(mid(r.R).X), float32(mid(r.R).Y))); c.kind != akV1 && c.kind != akV2 && hit.Kind != HitCard {
				t.Errorf("%s: HitAt on the image is %d, want HitCard", c.name, hit.Kind)
			}
		}
		if len(h.urls) != 0 {
			t.Errorf("%s: an image click opened %q in the browser", c.name, h.urls)
		}
	}

	// The rest of a link card is still its link.
	var at time.Duration
	hs, h := clickHarness(clickDoc(t, "large image"))
	card := cardBox(t, hs.v)
	hs.clickAt(image.Pt(card.Min.X+8, card.Max.Y-8), &at)
	if len(h.urls) != 1 || h.urls[0] != "https://blog.example.org/posts/gio-tips" || len(h.images) != 0 {
		t.Errorf("row card body: urls %q images %d", h.urls, len(h.images))
	}
	hs, h = clickHarness(clickDoc(t, "x post"))
	card = cardBox(t, hs.v)
	hs.clickAt(image.Pt(card.Max.X-8, card.Min.Y+8), &at)
	if len(h.urls) != 1 || h.urls[0] != "https://x.com/clackdevs/status/1987650000000000001" || len(h.images) != 0 {
		t.Errorf("x card body: urls %q images %d", h.urls, len(h.images))
	}

	// thumb_url alone in a row card: the card's link.
	hs, h = clickHarness(clickDoc(t, "small thumb"))
	if n := len(hs.v.ImageRects()); n != 0 {
		t.Errorf("small thumb: %d viewer images", n)
	}
	hs.clickAt(mid(imgPrimAt(t, hs.v, clickImg(150, 150, "news-thumb"))), &at)
	if len(h.urls) != 1 || h.urls[0] != "https://news.example.com/2026/11/native-clients" || len(h.images) != 0 {
		t.Errorf("row card thumb: urls %q images %d", h.urls, len(h.images))
	}

	// video_html's poster: the link (from_url).
	hs, h = clickHarness(clickDoc(t, "youtube"))
	hs.clickAt(mid(imgPrimAt(t, hs.v, clickImg(480, 360, "yt-thumb"))), &at)
	if len(h.urls) != 1 || h.urls[0] != "https://www.youtube.com/watch?v=c1ack0000000" || len(h.images) != 0 {
		t.Errorf("video poster: urls %q images %d", h.urls, len(h.images))
	}

	// thumb_url alone in a classic card: nothing.
	thumb := clickImg(80, 80, "bot-thumb")
	hs, h = clickHarness(attDoc("bot post", slack.Attachment{ID: 1, Color: "#36a64f", BotID: "B0FAKEBOT1", Title: "Build 12",
		TitleLink: site, Text: "passed", ThumbURL: thumb, ThumbWidth: 80, ThumbHeight: 80}))
	if k := kindOf(attBlock(t, attDoc("x", slack.Attachment{ID: 1, BotID: "B0FAKEBOT1", Title: "Build 12", Text: "passed", ThumbURL: thumb}))); k != akV1 {
		t.Errorf("classic thumb card: kind %d", k)
	}
	hs.clickAt(mid(imgPrimAt(t, hs.v, thumb)), &at)
	if h.total() != 0 {
		t.Errorf("classic card thumb: %+v", h)
	}
}
