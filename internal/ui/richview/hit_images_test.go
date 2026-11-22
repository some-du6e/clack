package richview

import (
	"encoding/json"
	"image"
	"strings"
	"testing"

	"gioui.org/f32"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// What a right-click on each kind of image in a message is over, as
// Slack's web client draws them (build a09b625d90aa) and its desktop
// shell builds the menu (main.bundle.cjs buildMenuForElement, 4.53.276):
//
//   - an image block (Image, 8plt) and a classic attachment's image_url
//     (AttachmentMedia → AttachmentImage, UvHM renderLink) are the CSS
//     background of an <a href>: the shell's link menu for the href,
//     the image block's image_url, the attachment's getExternalSourceUrl
//     (title_link, original_url, app_unfurl_url, a from_url off Slack's
//     hosts) or its image_url;
//   - Block Kit's image element (BaseImageElement: a section's accessory,
//     a context block's image) is a background div, no <img> and no
//     link: the message list's own menu;
//   - an emoji is an <img>: the shell's image menu.

func hitMid(v *View, r image.Rectangle) Hit {
	return v.HitAt(f32.Pt(float32(r.Min.X+r.Dx()/2)+.5, float32(r.Min.Y+r.Dy()/2)+.5))
}

func blocksDoc(js string) rt.Doc {
	m := slack.Message{Type: "message", TS: "1762700000.000200", User: "U0", Blocks: json.RawMessage(js)}
	return rt.FromMessage(codeRes{}, &m)
}

func TestHitAtSlackImageTargets(t *testing.T) {
	img := clickImg(400, 200, "hit")
	emojiLink := "https://emoji.slack-edge.com/T0266FRGM/party/0a1b2c3d4e5f6a7b.png"
	links := []struct {
		name string
		doc  rt.Doc
		link string
	}{
		{"image block", blocksDoc(`[{"type":"image","block_id":"i1","image_url":"` + img + `","alt_text":"chart"}]`), img},
		{"a bot's image link", attDoc("<"+emojiLink+">", slack.Attachment{ID: 1, Fallback: "128x128px image", Title: "party.png",
			FromURL: emojiLink, ImageURL: img, ImageWidth: 128, ImageHeight: 128}), emojiLink},
		{"title link", attDoc("", slack.Attachment{ID: 1, Title: "Chart", TitleLink: "https://example.com/chart",
			ImageURL: img, ImageWidth: 400, ImageHeight: 200}), "https://example.com/chart"},
		{"from a workspace link", attDoc("", slack.Attachment{ID: 1, Title: "Chart", FromURL: "https://hackclub.slack.com/archives/C0GENERAL/p1700000000000100",
			ImageURL: img, ImageWidth: 400, ImageHeight: 200}), img},
	}
	for _, c := range links {
		hs, _ := clickHarness(c.doc)
		rs := hs.v.ImageRects()
		if len(rs) != 1 {
			t.Errorf("%s: %d images", c.name, len(rs))
			continue
		}
		h := hitMid(hs.v, rs[0].R)
		if h.Kind != HitImage || hitLink(h) != c.link {
			t.Errorf("%s: kind %d link %q, want an image in the link %q", c.name, h.Kind, hitLink(h), c.link)
		}
	}

	// a section's accessory image: BaseImageElement, the message's menu
	acc := blocksDoc(`[{"type":"section","block_id":"s1","text":{"type":"mrkdwn","text":"build passed"},` +
		`"accessory":{"type":"image","image_url":"` + img + `","alt_text":"badge"}}]`)
	hs, _ := clickHarness(acc)
	rs := hs.v.ImageRects()
	if len(rs) != 1 {
		t.Fatalf("accessory: %d images", len(rs))
	}
	if h := hitMid(hs.v, rs[0].R); h.Kind != HitNone || h.EmojiURL != "" || hitLink(h) != "" {
		t.Errorf("accessory image: kind %d emoji %q link %q, want nothing (the message's menu)", h.Kind, h.EmojiURL, hitLink(h))
	}

	// a context block's image: the same element, inline
	ctx := blocksDoc(`[{"type":"context","block_id":"c1","elements":[{"type":"image","image_url":"` + img + `","alt_text":"logo"},` +
		`{"type":"mrkdwn","text":"from CI"}]}]`)
	hs, _ = clickHarness(ctx)
	found := false
	for i := range hs.v.d.frags {
		f := &hs.v.d.frags[i]
		if f.kind != fImage || f.alt || f.url == "" || strings.HasPrefix(f.url, iconURL) { // not an emoji or an icon
			continue
		}
		found = true
		p := f32.Pt(f.x+float32(f.size)/2, float32(f.y+f.size/2))
		if h := hs.v.HitAt(p); h.EmojiURL != "" || h.Kind != HitNone {
			t.Errorf("context image: kind %d emoji %q, want nothing (the message's menu)", h.Kind, h.EmojiURL)
		}
	}
	if !found {
		t.Fatal("the context block drew no image")
	}
}

// getExternalSourceUrl (Dv89 uN): title_link, then original_url, then
// app_unfurl_url, then a from_url not on a Slack workspace host, with a
// query that only escapes its ampersands unescaped (lodash unescape).
func TestExternalSourceURL(t *testing.T) {
	for _, c := range []struct {
		a    rt.Attachment
		want string
	}{
		{rt.Attachment{TitleLink: "https://t.example/", OriginalURL: "https://o.example/", FromURL: "https://f.example/"}, "https://t.example/"},
		{rt.Attachment{OriginalURL: "https://o.example/", AppUnfurlURL: "https://a.example/", FromURL: "https://f.example/"}, "https://o.example/"},
		{rt.Attachment{AppUnfurlURL: "https://a.example/", FromURL: "https://f.example/"}, "https://a.example/"},
		{rt.Attachment{FromURL: "https://emoji.slack-edge.com/T0/x/1.png"}, "https://emoji.slack-edge.com/T0/x/1.png"},
		{rt.Attachment{FromURL: "https://hackclub.slack.com/archives/C1/p1700000000000100"}, ""},
		{rt.Attachment{FromURL: "https://hackclub.enterprise.slack.com/archives/C1"}, ""},
		{rt.Attachment{FromURL: "https://app.slack.com/client/T0/C1"}, ""},
		{rt.Attachment{FromURL: "https://files.slack.com/files-pri/T0-F1/a.png"}, ""},
		{rt.Attachment{FromURL: "https://a.b.example.slack.com/x"}, "https://a.b.example.slack.com/x"},
		{rt.Attachment{TitleLink: "https://e.example/p?a=1&amp;b=2"}, "https://e.example/p?a=1&b=2"},
		{rt.Attachment{TitleLink: "https://e.example/p?a=1&amp;b=2&c=3"}, "https://e.example/p?a=1&amp;b=2&c=3"},
		{rt.Attachment{}, ""},
	} {
		if got := extSourceURL(&c.a); got != c.want {
			t.Errorf("%+v: %q, want %q", c.a, got, c.want)
		}
	}
}
