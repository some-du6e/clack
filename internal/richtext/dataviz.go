package richtext

import (
	"bytes"
	"encoding/json"
	"slices"
	"sync/atomic"

	"github.com/some-du6e/clack/internal/chart"
)

// DataViz is a data_visualization block as Slack's renderable reads it
// (boot-shared vxqh: processProps' toCamelCaseProps, then
// canvas-data-visualization-block's DataVisualizationBlock, build
// 9f450b8ad3db). Its fields come from the block's own keys, decoded on
// their own (bkDataViz): a title, block_id or size typed unlike another
// block's same key doesn't fail the block, and Slack reads them with
// JavaScript's semantics (package chart).
type DataViz struct {
	BlockID string
	// Title is the h3's text: the block's title as React draws it (a
	// number as its digits; null, a boolean or none as nothing).
	Title string
	// PlainTitle is the block's text in a notification
	// (convertDataVisualizationBlockToPlainText, the default verbosity:
	// t.title).
	PlainTitle string
	// Chart is the block's chart.
	Chart chart.Data
	// AriaLabel is the chart's accessible name: the block's aria_label,
	// else buildChartDescription.
	AriaLabel string
	// Size is the block's size ("small" or "large"): only the
	// size-variants layout reads it, the legacy one doesn't.
	Size string
	// PreviewLight and PreviewDark are preview_images' URLs: Copy as image
	// copies the theme's (falling back to the other).
	PreviewLight, PreviewDark string
	// Err: the render throws (a missing chart, a malformed one, an object
	// where React draws text): the surface's errorFallback draws instead.
	Err bool
}

// bkDataViz is a data_visualization block's keys, raw.
type bkDataViz struct {
	Title         json.RawMessage `json:"title"`
	Chart         json.RawMessage `json:"chart"`
	Size          json.RawMessage `json:"size"`
	PreviewImages json.RawMessage `json:"preview_images"`
	PreviewCamel  json.RawMessage `json:"previewImages"`
	AriaLabel     json.RawMessage `json:"aria_label"`
	AriaCamel     json.RawMessage `json:"ariaLabel"`
	BlockID       json.RawMessage `json:"block_id"`
}

// ownKeysType is a block type decoded by its renderable's own keys:
// data_visualization, data_table, timeline, contact_card, share_prompt,
// event and carousel.
func ownKeysType(t string) bool {
	switch t {
	case "data_visualization", "data_table", "timeline", "contact_card", "share_prompt", "event", "carousel":
		return true
	}
	return false
}

// ownKeysBlock decodes a block ownKeysType names by the keys its
// renderable reads only (bkDataViz, bkDataTable, bkTimeline,
// bkContactCard, bkSharePrompt, bkEvent, bkCarousel), so a key typed
// unlike another block's fails nothing; false when raw is none of them.
func ownKeysBlock(raw json.RawMessage) (bk, bool) {
	switch typ := partType(raw); typ {
	case "data_table":
		var v bkDataTable
		if json.Unmarshal(raw, &v) != nil {
			return bk{}, false
		}
		return bk{Type: typ, BlockID: rawString(v.BlockID), table: &v}, true
	case "data_visualization":
		var v bkDataViz
		if json.Unmarshal(raw, &v) != nil {
			return bk{}, false
		}
		return bk{Type: typ, BlockID: rawString(v.BlockID), viz: &v}, true
	case "timeline":
		var v bkTimeline
		if json.Unmarshal(raw, &v) != nil {
			return bk{}, false
		}
		return bk{Type: typ, BlockID: rawString(v.BlockID), timeline: &v}, true
	case "contact_card":
		var v bkContactCard
		if json.Unmarshal(raw, &v) != nil {
			return bk{}, false
		}
		return bk{Type: typ, BlockID: rawString(v.BlockID), contact: &v}, true
	case "share_prompt":
		var v bkSharePrompt
		if json.Unmarshal(raw, &v) != nil {
			return bk{}, false
		}
		return bk{Type: typ, BlockID: rawString(v.BlockID), prompt: &v}, true
	case "event":
		var v bkEvent
		if json.Unmarshal(raw, &v) != nil {
			return bk{}, false
		}
		return bk{Type: typ, BlockID: rawString(v.BlockID), event: &v}, true
	case "carousel":
		var v bkCarousel
		if json.Unmarshal(raw, &v) != nil {
			return bk{}, false
		}
		return bk{Type: typ, BlockID: rawString(v.BlockID), carousel: &v}, true
	}
	return bk{}, false
}

// partType is a block's "type" when it's a string, read off its keys up
// to that one (the rest isn't decoded: a part's nested blocks cost
// nothing here); "" otherwise.
func partType(raw json.RawMessage) string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return ""
	}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return ""
		}
		if k == "type" {
			var v any
			if dec.Decode(&v) != nil {
				return ""
			}
			s, _ := v.(string)
			return s
		}
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return ""
		}
	}
	return ""
}

// listDecodes counts the block lists decoded (decodeBlocks, child_blocks):
// the tests hold it to one per list (TestBlockDecodeLinear).
var listDecodes atomic.Int64

// decodePart decodes one block, once: a data or timeline block by its own keys, else
// as a struct (its child_blocks with it, each list once).
func decodePart(p json.RawMessage) (bk, error) {
	if ownKeysType(partType(p)) {
		if b, ok := ownKeysBlock(p); ok {
			return b, nil
		}
	}
	var b bk
	err := json.Unmarshal(p, &b)
	return b, err
}

// decodeBlocks decodes a message's (or a fallback's) list of blocks: one
// struct decode, and the data blocks among them again by their own keys
// (dataBlock). Every other node decodes as it always has, in the one
// pass. When that pass fails, each block on its own, once (decodePart),
// a bad one badBlock: a list nested in it doesn't decode again more than
// that once.
func decodeBlocks(raw json.RawMessage) ([]bk, error) {
	listDecodes.Add(1)
	var bs []bk
	err := json.Unmarshal(raw, &bs)
	if err == nil && !slices.ContainsFunc(bs, func(b bk) bool { return ownKeysType(b.Type) }) {
		return bs, nil
	}
	var parts []json.RawMessage
	if e := json.Unmarshal(raw, &parts); e != nil {
		return nil, e
	}
	if err == nil && len(parts) == len(bs) {
		for i := range bs {
			if ownKeysType(bs[i].Type) {
				if b, ok := ownKeysBlock(parts[i]); ok {
					bs[i] = b
				}
			}
		}
		return bs, nil
	}
	bs = make([]bk, 0, len(parts))
	for _, p := range parts {
		b, err := decodePart(p)
		if err != nil {
			b = bk{Type: badBlock}
		}
		bs = append(bs, b)
	}
	return bs, nil
}

// bkChildren is a callout's or container's child_blocks: each child
// decoded once (decodePart); one that doesn't decode fails the list, and
// with it the container, as a []bk would.
type bkChildren []bk

func (l *bkChildren) UnmarshalJSON(data []byte) error {
	listDecodes.Add(1)
	var parts []json.RawMessage
	if err := json.Unmarshal(data, &parts); err != nil {
		return err
	}
	bs := make([]bk, 0, len(parts))
	for _, p := range parts {
		b, err := decodePart(p)
		if err != nil {
			return err
		}
		bs = append(bs, b)
	}
	*l = bs
	return nil
}

// rawString is a raw JSON value when it is a string ("" otherwise).
func rawString(raw json.RawMessage) string {
	var id any
	if json.Unmarshal(raw, &id) == nil {
		if s, ok := id.(string); ok {
			return s
		}
	}
	return ""
}

// dataVizBlock builds a data_visualization block.
func (c *builder) dataVizBlock(b *bk) Block {
	v := b.viz
	if v == nil {
		v = &bkDataViz{}
	}
	dv := &DataViz{BlockID: b.BlockID}
	title, hasTitle := rawValue(v.Title)
	t, titleErr := chart.ReactText(title, hasTitle)
	dv.Title = t
	switch tt := title.(type) {
	case string:
		dv.PlainTitle = tt
	case float64:
		dv.PlainTitle = chart.JSNumber(tt)
	}
	dv.Chart = chart.ParseChart(v.Chart)
	dv.Err = titleErr != nil || dv.Chart.Err != nil
	if s, ok := rawValue(v.Size); ok {
		if str, ok := s.(string); ok {
			dv.Size = str
		}
	}
	pi := v.PreviewImages
	if len(pi) == 0 {
		pi = v.PreviewCamel
	}
	var imgs map[string]any
	if json.Unmarshal(pi, &imgs) == nil {
		dv.PreviewLight = stringKey(imgs, "preview_image_light_mode", "previewImageLightMode")
		dv.PreviewDark = stringKey(imgs, "preview_image_dark_mode", "previewImageDarkMode")
	}
	al := v.AriaLabel
	if len(al) == 0 {
		al = v.AriaCamel
	}
	if a, ok := rawValue(al); ok && a != nil {
		// aria-label={ariaLabel ?? description}: an attribute is its String()
		dv.AriaLabel = chart.JSString(a)
	} else if !dv.Err {
		dv.AriaLabel = dv.Chart.Description(title, hasTitle)
	}
	return Block{Kind: DataVizBlock, BlockID: b.BlockID, DataViz: dv}
}

// rawValue decodes a raw JSON value (false: the key was absent).
func rawValue(raw json.RawMessage) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil, false
	}
	return v, true
}

func stringKey(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok {
			return s
		}
	}
	return ""
}
