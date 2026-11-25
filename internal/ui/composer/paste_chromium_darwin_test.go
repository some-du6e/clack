//go:build darwin && !ios && cgo

package composer

import (
	"encoding/json"
	"image"
	"image/color"
	"os/exec"
	"slices"
	"testing"

	"github.com/some-du6e/clack/internal/ui/clipimg"
)

// macPBState is the general pasteboard as JXA reads it, cleared nothing:
// how many items it has, the first's types, its public.html and plain
// text (nil when it has none), and whether it offers a TIFF.
type macPBState struct {
	Items int      `json:"items"`
	Types []string `json:"types"`
	HTML  *string  `json:"html"`
	Text  *string  `json:"text"`
	TIFF  bool     `json:"tiff"`
}

func macPBRead(t *testing.T) macPBState {
	t.Helper()
	const script = `ObjC.import('AppKit');
function run() {
	var pb = $.NSPasteboard.generalPasteboard;
	var items = pb.pasteboardItems;
	var n = parseInt(String(items.count), 10); // a bridged NSUInteger: JSON had it as a string
	var types = n > 0 ? ObjC.deepUnwrap(items.objectAtIndex(0).types) : [];
	var html = ObjC.unwrap(pb.stringForType('public.html'));
	var text = ObjC.unwrap(pb.stringForType('public.utf8-plain-text'));
	var tiff = ObjC.unwrap(pb.availableTypeFromArray($.NSArray.arrayWithObject('public.tiff')));
	return JSON.stringify({items: n, types: types, html: html === undefined ? null : html,
		text: text === undefined ? null : text, tiff: tiff !== undefined});
}`
	out, err := exec.Command("osascript", "-l", "JavaScript", "-e", script).CombinedOutput()
	if err != nil {
		t.Fatalf("osascript: %v\n%s", err, out)
	}
	var s macPBState
	if err := json.Unmarshal(out, &s); err != nil {
		t.Fatalf("pasteboard state %q: %v", out, err)
	}
	t.Logf("pasteboard: %d items, first %q, TIFF offered %v", s.Items, s.Types, s.TIFF)
	return s
}

// The real pasteboard (ci's macos job) with what Copy Image puts there,
// as Slack's desktop app's Chromium does (ClipboardMac, Chromium 156:
// clipimg/chromium.go): one item, public.png of the first frame (given
// to writeObjects:, which offers it as a TIFF too), and public.html,
// "<meta charset='utf-8'>" and the <img> markup; no bookmark on macOS
// (Blink writes none there: crbug.com/838808) and no text. The
// composer's paste takes the PNG as the paste's file (image.png, its
// alpha kept), as Slack's useUploadPastedFiles does; with no text the
// paste inserts nothing (TestPasteChromiumImageInsertsNothing). A file
// menu's Copy image (WriteCharged) is the PNG alone.
func TestMacPasteChromiumImage(t *testing.T) {
	macPBEnabled(t)
	img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for at, c := range macPBOpaque {
		img.SetNRGBA(at[0], at[1], c)
	}
	img.SetNRGBA(2, 1, color.NRGBA{0xff, 0, 0, 0x80})
	tag := &clipimg.Tag{URL: "https://slack-imgs.com/?c=1&o1=ro&url=https%3A%2F%2Fmedia3.giphy.com%2Fmedia%2Fx%2Fgiphy.webp", Title: "A test GIF"}
	if err := clipimg.WriteTagged(macPBCtx(t), img, tag, nil); err != nil {
		t.Fatalf("clipimg.WriteTagged: %v", err)
	}
	s := macPBRead(t)
	if s.Items != 1 || !slices.Contains(s.Types, "public.png") || !slices.Contains(s.Types, "public.html") {
		t.Errorf("%d items, the first %q: want one, with public.png and public.html", s.Items, s.Types)
	}
	for _, no := range []string{"public.url", "public.file-url", "public.utf8-plain-text"} {
		if slices.Contains(s.Types, no) {
			t.Errorf("the item has %s (types %q): Chromium writes none", no, s.Types)
		}
	}
	const html = `<meta charset='utf-8'><img src="https://slack-imgs.com/?c=1&amp;o1=ro&amp;url=https%3A%2F%2Fmedia3.giphy.com%2Fmedia%2Fx%2Fgiphy.webp" alt="A test GIF"/>`
	if s.HTML == nil || *s.HTML != html {
		t.Errorf("public.html %v, want %q", s.HTML, html)
	}
	if s.Text != nil { // not the item's (its types say so): a conversion the pasteboard offers
		t.Logf("the pasteboard offers plain text %q", *s.Text)
	}
	data, err := macPBImage(t, false)
	if err != nil {
		t.Fatalf("attClipboardImage: %v (Chromium's copy isn't an office app's)", err)
	}
	px := macPBPixels(t, data)
	for at, want := range macPBOpaque {
		if !macPBNear(px[at], want) {
			t.Errorf("pixel %v = %v, want %v", at, px[at], want)
		}
	}
	if got := px[[2]int{2, 1}]; !macPBNear(got, color.NRGBA{0xff, 0, 0, 0x80}) {
		t.Errorf("translucent pixel %v, want the PNG's alpha 0x80", got)
	}

	if err := clipimg.WriteCharged(macPBCtx(t), img, nil); err != nil {
		t.Fatalf("clipimg.WriteCharged: %v", err)
	}
	if s := macPBRead(t); s.Items != 1 || !slices.Contains(s.Types, "public.png") || s.HTML != nil {
		t.Errorf("the bitmap alone: %d items, the first %q, html %v: want public.png alone", s.Items, s.Types, s.HTML)
	}
}
