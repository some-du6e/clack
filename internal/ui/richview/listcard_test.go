package richview

import (
	"image"
	"testing"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// A list shared in a message is Slack's compact list entity (listcard.go):
// 62px, at most 328 wide alone, the honeycomb list thumbnail with the
// lists-filled glyph, and a click target that opens the file.
func TestListFileCard(t *testing.T) {
	f := &rt.File{ID: "F0LIST00001", Title: "Launch tracker", Type: "list", PrettyType: "List"}
	v := layoutDoc(t, rt.Doc{Blocks: []rt.Block{{Kind: rt.FileBlock, File: f}}}, 900, Options{})
	r := cardBox(t, v)
	if r.Dx() != 328 || r.Dy() != 62 {
		t.Errorf("card %v, want 328x62", r)
	}
	thumb := false
	for _, b := range v.d.boxes {
		if b.kind == bFill && b.r == image.Rect(r.Min.X+13, r.Min.Y+13, r.Min.X+49, r.Min.Y+49) && b.fill == rgb(225, 155, 6) {
			thumb = true
		}
	}
	if !thumb {
		t.Error("no honeycomb-40 thumbnail at the card's padding")
	}
	glyph := false
	for _, im := range v.d.imgs {
		glyph = glyph || im.url == iconURL+"lists-filled"
	}
	if !glyph {
		t.Error("no lists-filled glyph")
	}
	opens := false
	for _, tg := range v.d.targets {
		opens = opens || (tg.kind == tFile && tg.file == f)
	}
	if !opens {
		t.Error("the card doesn't open the list")
	}
	// a narrower message keeps the card inside it
	v = layoutDoc(t, rt.Doc{Blocks: []rt.Block{{Kind: rt.FileBlock, File: f}}}, 240, Options{})
	if r := cardBox(t, v); r.Dx() > 240 || r.Dy() != 62 {
		t.Errorf("narrow card %v", r)
	}
}
