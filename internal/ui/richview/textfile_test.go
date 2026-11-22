package richview

import (
	"strings"
	"testing"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// Karim (2026-11-18): information.txt, which Slack typed "binary"
// (application/octet-stream, pretty_type "Binary"), drew a "BINA" text
// tile. Slack's file card draws FileEntityThumbnailIcon's svgicon for the
// filetype (getSVGIconType: binary-4f9150d.svg, a #616061 tile with the
// white 01; a "text" file file-snippet-0a4c72a.svg, #1D9BD1 with the T),
// the name, and getFileTypeDescription: the pretty_type. A hosted text
// file is a file card, not a snippet (only mode "snippet" is: getFileType).
// The meta line above one generic file (renderSingleFileTitle) is its
// pretty_type expanded and its title collapsed.
func TestTextFileCards(t *testing.T) {
	st := newTestStyle()
	for _, c := range []struct {
		f     *rt.File
		tile  [3]uint8
		glyph string
		kind  string
	}{
		{&rt.File{ID: "F0BIN", Name: "information.txt", Type: "binary", PrettyType: "Binary", Mime: "application/octet-stream", Size: 244542, Mode: "hosted"},
			[3]uint8{0x61, 0x60, 0x61}, "file-binary", "Binary"},
		{&rt.File{ID: "F0TXT", Name: "notes.txt", Type: "text", PrettyType: "Plain Text", Mime: "text/plain", Size: 1820, Mode: "hosted"},
			[3]uint8{0x1d, 0x9b, 0xd1}, "file-text", "Plain Text"},
	} {
		v := layoutAt(st, doc(fileBlock(c.f)), Options{CompactFiles: true}, 600)
		d := &v.d
		if cards := boxesOf(d, bBorder); len(cards) != 1 || cards[0].r.Dx() != 426 || cards[0].r.Dy() != 62 {
			t.Errorf("%s: cards %+v, want one 426x62 file card", c.f.Name, cards)
		}
		var tile *box
		for i := range d.boxes {
			if b := &d.boxes[i]; b.kind == bFill && b.r.Dx() == 36 && b.r.Dy() == 36 {
				tile = b
			}
		}
		if tile == nil || [3]uint8{tile.fill.R, tile.fill.G, tile.fill.B} != c.tile {
			t.Errorf("%s: tile %+v, want the svgicon's %x", c.f.Name, tile, c.tile)
		}
		if !hasIcon(d, c.glyph) {
			t.Errorf("%s: no %s glyph; icons %+v", c.f.Name, c.glyph, d.overlayIcons)
		}
		for _, f := range textFrags(d) {
			if f.sh.str == "BINA" || f.sh.str == "TEXT" {
				t.Errorf("%s: the type tile's %q is drawn", c.f.Name, f.sh.str)
			}
		}
		drawn := strings.Join(fragStrings(d), " ")
		if !strings.Contains(drawn, c.f.Name) {
			t.Errorf("%s: no name; frags %q", c.f.Name, drawn)
		}
		if !strings.Contains(drawn, c.kind) {
			t.Errorf("%s: no %q type line; frags %q", c.f.Name, c.kind, drawn)
		}
		if got := fileMetaText([]*rt.File{c.f}, true); got != c.kind {
			t.Errorf("%s: meta line %q expanded, want the pretty_type %q", c.f.Name, got, c.kind)
		}
		if got := fileMetaText([]*rt.File{c.f}, false); got != c.f.Name {
			t.Errorf("%s: meta line %q collapsed, want the title", c.f.Name, got)
		}
	}
}
