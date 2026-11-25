package richtext

// A carousel block: Slack's CarouselBlock (boot-shared ZDqH; CDN build
// 9f450b8ad3db): its elements, each a block (a card, as Slack's own
// carousels send), drawn side by side in a sideways scroller. Its props go
// through toCamelCaseProps, so label and is_label_visible take either
// spelling. Elements that aren't a list are `(a || []).map` throwing: the
// block's error line.

import "encoding/json"

// Carousel is a CarouselBlock's own props (its cards are the Block's
// Children).
type Carousel struct {
	// Label is the group's label ("carousel" when it has none);
	// LabelVisible draws it above the scroller (isLabelVisible && label).
	Label        string
	LabelVisible bool
}

// bkCarousel is a carousel block's keys, raw.
type bkCarousel struct {
	Elements     json.RawMessage `json:"elements"`
	Label        json.RawMessage `json:"label"`
	Visible      json.RawMessage `json:"is_label_visible"`
	VisibleCamel json.RawMessage `json:"isLabelVisible"`
	BlockID      json.RawMessage `json:"block_id"`
}

// carouselBlock builds a carousel block.
func (c *builder) carouselBlock(out []Block, b *bk) []Block {
	v := b.carousel
	if v == nil {
		v = &bkCarousel{}
	}
	cr := &Carousel{Label: rawString(v.Label)}
	if vis, ok := rawValue(firstRaw(v.Visible, v.VisibleCamel)); ok {
		cr.LabelVisible = jsTruthy(vis) && cr.Label != ""
	}
	blk := Block{Kind: CarouselBlock, BlockID: b.BlockID, Carousel: cr}
	if raw, ok := rawValue(v.Elements); ok && jsTruthy(raw) {
		if _, isList := raw.([]any); !isList {
			return append(out, unsupported()) // a.map of a non-list
		}
		parts, err := decodeBlocks(v.Elements)
		if err != nil {
			return append(out, unsupported())
		}
		blk.Children = c.blocks(parts)
	}
	return append(out, blk)
}
