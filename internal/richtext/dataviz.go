package richtext

import (
	"encoding/json"

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

// UnmarshalJSON decodes a block, and a data_visualization or data_table
// block by the keys its renderable reads only.
func (b *bk) UnmarshalJSON(data []byte) error {
	type plain bk
	err := json.Unmarshal(data, (*plain)(b))
	typ := b.Type
	if err != nil {
		var t struct {
			Type any `json:"type"`
		}
		if json.Unmarshal(data, &t) != nil {
			return err
		}
		typ, _ = t.Type.(string)
	}
	if typ == "data_table" {
		var t bkDataTable
		if json.Unmarshal(data, &t) != nil {
			return err
		}
		*b = bk{Type: typ, table: &t}
		b.BlockID = rawString(t.BlockID)
		return nil
	}
	if typ != "data_visualization" {
		return err
	}
	var v bkDataViz
	if json.Unmarshal(data, &v) != nil {
		return err
	}
	*b = bk{Type: typ, viz: &v}
	b.BlockID = rawString(v.BlockID)
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
