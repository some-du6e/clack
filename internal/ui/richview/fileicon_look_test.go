package richview

import (
	"image/color"
	"testing"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
)

// IconThumbnail's tiles (CDN build bv1-13, boot-shared PQXV), in Slack's
// kit colours: QuipIcon's canvas-content-filled lagoon-0 on lagoon-40
// (lagoon-100 on lagoon-60 dark), BaseListIcon's lists-filled
// honeycomb-0 on honeycomb-40 (honeycomb-100 on honeycomb-50 dark), a
// folder's white folder-filled on gray-50, a skill set's indigo-60 book
// on indigo-5; anything else the svgicon of its filetype.
func TestFileIconLook(t *testing.T) {
	rgb := func(c uint32) color.NRGBA {
		return color.NRGBA{R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c), A: 0xff}
	}
	white := rgb(0xffffff)
	for _, c := range []struct {
		name   string
		f      richtext.File
		dark   bool
		bg, fg color.NRGBA
		glyph  string
	}{
		{"canvas", richtext.File{Type: "quip", Mode: "quip"}, false, rgb(0x1cb6eb), rgb(0xe3f8ff), "canvas-content-filled"},
		{"canvas", richtext.File{Type: "quip", Mode: "quip"}, true, rgb(0x2ba5ce), rgb(0xd8eef6), "canvas-content-filled"},
		{"canvas by mode", richtext.File{Mode: "quip"}, false, rgb(0x1cb6eb), rgb(0xe3f8ff), "canvas-content-filled"},
		{"list", richtext.File{Type: "list", Mode: "list"}, false, rgb(0xe19b06), rgb(0xfcf2d9), "list-tab-filled"},
		{"list by mode", richtext.File{Mode: "list"}, true, rgb(0xc48305), rgb(0xf4e9ce), "list-tab-filled"},
		{"folder", richtext.File{Type: "folder"}, false, rgb(0x7c7a7f), white, "folder-filled"},
		{"folder", richtext.File{Type: "folder"}, true, rgb(0x797c81), white, "folder-filled"},
		{"skill set", richtext.File{Type: "folder_skill_set"}, false, rgb(0xdce3fa), rgb(0x3651c0), "book"},
		{"skill set", richtext.File{Type: "folder_skill_set"}, true, rgb(0x0a165a), rgb(0x657ace), "book"},
	} {
		lk := FileIconLook(&c.f, c.dark)
		if lk.Art != "" || lk.Bg != c.bg || lk.Fg != c.fg || lk.Glyph != c.glyph {
			t.Errorf("%s (dark %v): %+v, want %q in %v on %v", c.name, c.dark, lk, c.glyph, c.fg, c.bg)
		}
		if !icons.Has(lk.Glyph) && !icons.HasSlack(lk.Glyph) {
			t.Errorf("%s: glyph %q isn't in the set", c.name, lk.Glyph)
		}
	}
	for _, c := range []struct {
		f   richtext.File
		art string
	}{
		{richtext.File{Type: "docx"}, "fileicon-file-word"},
		{richtext.File{Type: "exe"}, "fileicon-generic"},
		{richtext.File{Type: "binary"}, "fileicon-binary"},
		{richtext.File{Type: "pdf"}, "fileicon-file-pdf"},
	} {
		for _, dark := range []bool{false, true} {
			if lk := FileIconLook(&c.f, dark); lk.Art != c.art || lk.Glyph != "" {
				t.Errorf("%s (dark %v): %+v, want the %q svgicon", c.f.Type, dark, lk, c.art)
			}
		}
	}
}
