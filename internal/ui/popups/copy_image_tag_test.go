package popups

import (
	"image"
	"net/url"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/clipimg"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Copy Image on the GIF picker's image (Karim's report): what the clipboard
// write gets (copy_image_fixture_test.go has the fixture).

// With animations off the GIF is AttachmentImage's PlayPauseAnimations, an
// <img src={source} alt={title}>: its src the proxy's still
// (getImgProxyUrl rotate + gu), its alt the block's alt_text. Copy Image
// copies the first frame with that src and alt beside it, and Copy Image
// URL that src; while it plays, the src is the animated source (Ac).
func TestCopyImageGIFPickerStill(t *testing.T) {
	h := &tzHost{th: theme.New(false)}
	got, tag := copyCatchTag(t, gpFetch)
	img := &richtext.Image{URL: gpImage, Alt: gpAlt, Title: "GIF", W: 220, H: 260}
	m := NewImageMenuOf(h, image.Pt(10, 10), contentImage(richview.Hit{Kind: richview.HitImage, Image: img, Played: true}))
	if l := ctxLabels(m); l != "Copy Image|Copy Image URL" {
		t.Fatalf("menu %q", l)
	}
	still := "https://slack-imgs.com/?c=1&o1=ro.gu&url=" + gpQuery
	m.Items()[0].Do()
	firstFrame(t, waitCopy(t, h, got), "Copy Image")
	if tg := tag(); tg == nil || *tg != (clipimg.Tag{URL: still, Title: gpAlt}) {
		t.Errorf("Copy Image's tag %+v, want the <img>'s src %q and alt %q", tg, still, gpAlt)
	}
	if u := emClip(m.Items()[1]); u != still {
		t.Errorf("Copy Image URL copied %q, want the <img>'s src %q", u, still)
	}

	playing := NewImageMenuOf(h, image.Pt(10, 10), contentImage(richview.Hit{Kind: richview.HitImage, Image: img, Played: true, Playing: true}))
	animated := "https://slack-imgs.com/?c=1&o1=ro&url=" + gpQuery
	if u := emClip(playing.Items()[1]); u != animated {
		t.Errorf("playing: Copy Image URL copied %q, want the animated source %q", u, animated)
	}
	if im, ok := playing.Items()[0].CopiesImage(); !ok || im.Src != animated || im.Alt != gpAlt || im.Shown != gpImage {
		t.Errorf("playing: Copy Image is for %+v (%v)", im, ok)
	}
}

// The image block's alt_text is the <img>'s alt; without one, its title.
func TestContentImageAlt(t *testing.T) {
	for _, c := range []struct{ alt, title, want string }{{"cat", "GIF", "cat"}, {"", "GIF", "GIF"}, {"", "", ""}} {
		im := contentImage(richview.Hit{Kind: richview.HitImage, Image: &richtext.Image{URL: gpImage, Alt: c.alt, Title: c.title}})
		if im.Alt != c.want || im.Src != gpImage {
			t.Errorf("alt %q title %q: %+v, want alt %q", c.alt, c.title, im, c.want)
		}
	}
}

// A file menu's Copy image is Slack's own copy (navigator.clipboard.write
// of an image/png): the first frame alone, no tag.
func TestFileCopyImageIsBitmapOnly(t *testing.T) {
	srv, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	h := &tzHost{a: core.New(slack.New(srv.Creds()), store.New("")), th: theme.New(false)}
	f := &richtext.File{ID: "F0GIFPICK1", Name: "a.webp", Mime: "image/webp", IsImage: true, User: fakeslack.ProfileID,
		URL: gpImage, Permalink: "https://x.slack.com/files/U/F0GIFPICK1/a.webp"}
	got, tag := copyCatchTag(t, gpFetch)
	m := NewFileMenu(h, image.Pt(10, 10), core.Message{Channel: "C1", TS: "1700000000.000100", User: fakeslack.ProfileID}, f)
	var cp *MenuItem
	for i, it := range m.Items() {
		if it.Label == "Copy image" {
			cp = &m.Items()[i]
		}
	}
	if cp == nil {
		t.Fatalf("no Copy image in %q", ctxLabels(m))
	}
	cp.Do()
	firstFrame(t, waitCopy(t, h, got), "Copy image")
	if tg := tag(); tg != nil {
		t.Errorf("the file's Copy image wrote a tag %+v, want the bitmap alone", tg)
	}
}

// The image viewer's <img> has the image's altText for alt: none for a
// message's image (its media item has none), a file's getFileAltText
// (its alt text, "(AI generated)" when an AI wrote it, else its name).
func TestLightboxCopyImageTag(t *testing.T) {
	const shot = "https://bucket.s3.example.com/shots/a.png"
	pngFetch := func(string) ([]byte, error) { return pngOf(t, 9, 6), nil }
	unfurl := core.Message{Channel: "C1", TS: "1762700000.000200", Doc: richtext.Doc{Blocks: []richtext.Block{
		{Kind: richtext.AttachmentBlock, Att: &richtext.Attachment{ID: "1", Legacy: true, FromURL: shot, OriginalURL: shot,
			Image: &richtext.Image{URL: shot, W: 900, H: 600, Alt: "not the viewer's"}}},
	}}}
	copyTag := func(e *lbEnv) *clipimg.Tag {
		t.Helper()
		got, tag := copyCatchTag(t, pngFetch)
		e.rightClickMiddle()
		ms := e.menus()
		if len(ms) != 1 {
			t.Fatalf("%d menus", len(ms))
		}
		ms[0].Items()[0].Do()
		waitCopy(t, e.h, got)
		return tag()
	}
	e := newLBEnv(t, richtext.Image{URL: shot, W: 900, H: 600}, unfurl)
	want := clipimg.Tag{URL: "https://slack-imgs.com/?c=1&o1=ro&url=" + url.QueryEscape(shot)}
	if tg := copyTag(e); tg == nil || *tg != want {
		t.Errorf("message image: tag %+v, want %+v", tg, want)
	}

	for _, c := range []struct {
		alt   string
		ai    bool
		title string
	}{{"A grey cat asleep on a keyboard", false, "A grey cat asleep on a keyboard"}, {"A cat", true, "A cat (AI generated)"}, {"", false, "photo.png"}} {
		file := &richtext.File{ID: "F0TEST0009", Name: "photo.png", Mime: "image/png", IsImage: true, AltText: c.alt, AltTextAI: c.ai,
			URL: "https://files.slack.com/files-pri/T1-F0TEST0009/photo.png", Permalink: "https://fake.slack.com/files/U1/F0TEST0009/photo.png",
			Thumb: &richtext.Image{URL: "https://files.slack.com/files-tmb/T1-F0TEST0009-abc/photo_360.png", W: 360, H: 240, Proxy: true}}
		fm := core.Message{Channel: "C1", TS: "1762700000.000300", Doc: richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.FileBlock, File: file}}}}
		e := newLBEnv(t, *file.Thumb, fm)
		tg := copyTag(e)
		if tg == nil || tg.Title != c.title || tg.URL == "" {
			t.Errorf("file with alt %q (AI %v): tag %+v, want the title %q", c.alt, c.ai, tg, c.title)
		}
	}
}
