package popups

import (
	"net/url"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

// An unfurl's image_url opens in the viewer as Slack's AttachmentImage
// opens it: a mediaItem, not a file. MediaViewerActions then has only
// Show thread and Open in new window (openInNewWindow opens image.src,
// the image through Slack's image proxy), no Download or More actions,
// no zoom control, and the header has no "– title".
func TestLightboxUnfurlImage(t *testing.T) {
	const shot = "https://bucket.s3.example.com/shots/a.png"
	file := &richtext.File{ID: "F0TEST0001", Name: "photo.png", Title: "photo.png", Mime: "image/png", IsImage: true,
		URL: "https://files.slack.com/files-pri/T1-F0TEST0001/photo.png", Permalink: "https://fake.slack.com/files/U1/F0TEST0001/photo.png",
		Thumb: &richtext.Image{URL: "https://files.slack.com/files-tmb/T1-F0TEST0001-abc/photo_360.png", W: 360, H: 240, Proxy: true}}
	m := core.Message{Channel: "C1", TS: "1762700000.000200", Doc: richtext.Doc{Blocks: []richtext.Block{
		{Kind: richtext.FileBlock, File: file},
		{Kind: richtext.AttachmentBlock, Att: &richtext.Attachment{ID: "1", Legacy: true, FromURL: shot, OriginalURL: shot,
			Image: &richtext.Image{URL: shot, W: 1440, H: 900}}},
	}}}
	lb := NewLightbox(nil, richtext.Image{URL: shot, W: 1440, H: 900}, m)
	if len(lb.items) != 2 {
		t.Fatalf("%d items, want the file and the unfurl image", len(lb.items))
	}
	it := lb.cur()
	if it.img.URL != shot || it.file != nil {
		t.Fatalf("opened on %+v, want the unfurl image", it)
	}
	if it.hosted() {
		t.Error("an unfurl image counts as a Slack file")
	}
	want := "https://slack-imgs.com/?c=1&o1=ro&url=" + url.QueryEscape(shot)
	if got := it.windowURL(); got != want {
		t.Errorf("Open in new window: %s, want %s", got, want)
	}
	lb.setZoom(1)
	lb.wheel(-200)
	if lb.zoom != 1 {
		t.Errorf("the wheel zoomed an unfurl image to %v", lb.zoom)
	}

	// the file next to it keeps the file's actions and link
	f := lb.items[0]
	if f.file != file || !f.hosted() {
		t.Fatalf("item 0 %+v isn't the file", f)
	}
	if got := f.windowURL(); got != file.Permalink {
		t.Errorf("file: Open in new window %s, want the permalink", got)
	}
	// an image the Files browser opens (a files.slack.com thumb, no file)
	if !(lbItem{img: richtext.Image{URL: file.URL, Proxy: true}}).hosted() {
		t.Error("a files.slack.com image isn't treated as a file")
	}
}
