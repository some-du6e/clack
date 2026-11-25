//go:build windows

package composer

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/ui/clipimg"
)

// The real clipboard (CI's windows-applog job) with what Copy Image puts
// there, as Slack's desktop app's Chromium does (clipimg: "PNG", CF_DIBV5,
// "HTML Format" with the <img> markup, "UniformResourceLocatorW"; no
// text). The composer's paste reads it as Slack's composer gets it from
// Chromium (text/html and an image/png file, no text/plain): the "PNG"
// as the paste's file, image.png, with its alpha; the markup as the rich
// read; no text. TestPasteChromiumImageInsertsNothing has the rest: such
// a paste inserts nothing.
func TestWinClipboardChromiumImage(t *testing.T) {
	winClipEnabled(t)
	img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for at, c := range winClipOpaque {
		img.SetNRGBA(at[0], at[1], c)
	}
	img.SetNRGBA(2, 1, color.NRGBA{0xff, 0, 0, 0x80})
	tag := &clipimg.Tag{URL: "https://slack-imgs.com/?c=1&o1=ro&url=https%3A%2F%2Fmedia3.giphy.com%2Fmedia%2Fx%2Fgiphy.webp", Title: "A test GIF"}
	if err := clipimg.WriteTagged(winClipCtx(t), img, tag, nil); err != nil {
		t.Fatalf("clipimg.WriteTagged: %v", err)
	}

	s := startPasteSnap(false)
	<-s.done
	if len(s.files) != 0 || s.text != "" {
		t.Errorf("files %q, text %q: want neither", s.files, s.text)
	}
	if s.imgErr != nil || s.imgMime != "image/png" || attPastedName(s.imgMime) != "image.png" {
		t.Fatalf("the paste's image: %v (%s, named %q)", s.imgErr, s.imgMime, attPastedName(s.imgMime))
	}
	got, err := png.Decode(bytes.NewReader(s.img))
	if err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	if got.Bounds().Size() != image.Pt(3, 2) {
		t.Fatalf("size %v, want 3x2", got.Bounds().Size())
	}
	for at, want := range winClipOpaque {
		if c := color.NRGBAModel.Convert(got.At(at[0], at[1])).(color.NRGBA); c != want {
			t.Errorf("pixel %v = %v, want %v", at, c, want)
		}
	}
	if c := color.NRGBAModel.Convert(got.At(2, 1)).(color.NRGBA); c != (color.NRGBA{0xff, 0, 0, 0x80}) {
		t.Errorf("translucent pixel = %v, want the PNG's alpha 0x80", c)
	}
	const markup = `<img src="https://slack-imgs.com/?c=1&amp;o1=ro&amp;url=https%3A%2F%2Fmedia3.giphy.com%2Fmedia%2Fx%2Fgiphy.webp" alt="A test GIF"/>`
	if s.rich == nil || !strings.Contains(s.rich.html, markup) {
		var h string
		if s.rich != nil {
			h = s.rich.html
		}
		t.Errorf("rich read %q, want the markup %q", h, markup)
	}
	if _, err := winClipImage(t, false); err != nil {
		t.Errorf("attClipboardImage: %v (Chromium's copy isn't an office app's: no rich text)", err)
	}
}
