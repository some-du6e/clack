package richtext

import "testing"

// Synthetic fixtures only.

// A carousel's elements are its cards, built as blocks of their own
// (Kit); label and is_label_visible take either spelling; elements that
// aren't a list are the block's error line, and no elements an empty
// carousel.
func TestCarouselDecode(t *testing.T) {
	card := func(id string) string {
		return `{"type":"card","block_id":"` + id + `","title":{"type":"plain_text","text":"Card ` + id + `"}}`
	}
	for _, extra := range []string{`"label":"Picks","is_label_visible":true`, `"label":"Picks","isLabelVisible":true`} {
		d := blocksMsg(t, `[{"type":"carousel","block_id":"cr",`+extra+`,"elements":[`+card("a")+`,`+card("b")+`]}]`)
		if len(d.Blocks) != 1 || d.Blocks[0].Kind != CarouselBlock || d.Blocks[0].Carousel == nil {
			t.Fatalf("%s: %s", extra, dump(d.Blocks))
		}
		b := d.Blocks[0]
		if b.BlockID != "cr" || b.Carousel.Label != "Picks" || !b.Carousel.LabelVisible {
			t.Errorf("%s: carousel %+v", extra, b.Carousel)
		}
		if len(b.Children) != 2 || b.Children[0].Kind != CardBlock || !b.Children[0].Kit || b.Children[1].BlockID != "b" {
			t.Errorf("%s: cards %s", extra, dump(b.Children))
		}
	}
	if d := blocksMsg(t, `[{"type":"carousel","label":"Picks","elements":[`+card("a")+`]}]`); d.Blocks[0].Carousel.LabelVisible {
		t.Errorf("a label without is_label_visible shows")
	}
	if d := blocksMsg(t, `[{"type":"carousel","is_label_visible":true,"elements":[`+card("a")+`]}]`); d.Blocks[0].Carousel.LabelVisible {
		t.Errorf("is_label_visible without a label shows one")
	}
	if d := blocksMsg(t, `[{"type":"carousel","elements":"x"}]`); len(d.Blocks) != 1 || d.Blocks[0].Kind != Unsupported {
		t.Errorf("elements a string: %s, want the error line", dump(d.Blocks))
	}
	if d := blocksMsg(t, `[{"type":"carousel"}]`); len(d.Blocks) != 1 || d.Blocks[0].Kind != CarouselBlock || len(d.Blocks[0].Children) != 0 {
		t.Errorf("no elements: %s, want an empty carousel", dump(d.Blocks))
	}
}
