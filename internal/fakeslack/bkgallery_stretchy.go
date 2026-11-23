package fakeslack

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
)

// The stretchy-emoji modal (Karim, #clack 2026-11-22: "is the spacing
// intended?"), shaped like a stretchy custom emoji app's: a multiline
// input, an actions block with a select and two checkboxes, a preview of a
// word spelled in custom emoji tiles that are drawn to join up edge to
// edge, a Copy button, the emoji string in a code block, and Done to
// close (global shortcut SURFA7). Synthetic: the tiles are the fake's own
// (stretchImage), each a bar across its whole width at mid height, so a
// seam between two shows as a break in the bar. The tiles are custom emoji
// of a Gallery-sized fake.

// SurfStretchyView is that modal's view id.
const SurfStretchyView = "VSURF00007"

// StretchyEmoji are the tiles' names: the left end, the middle, the right
// end.
var StretchyEmoji = []string{"stretchy-l", "stretchy-m", "stretchy-r"}

// StretchyBar is the tiles' bar colour.
var StretchyBar = color.NRGBA{0x6c, 0x4a, 0xd8, 0xff}

// StretchyCode is the preview's emoji string, as the code block holds it.
const StretchyCode = ":stretchy-l::stretchy-m::stretchy-m::stretchy-m::stretchy-m::stretchy-r:"

// addStretchyEmoji lists the tiles in emoji.list.
func (s *Server) addStretchyEmoji() {
	for _, n := range StretchyEmoji {
		s.emoji[n] = s.URL + "img/64x64/stretch-" + n + ".png"
	}
}

// SurfStretchy is that view; its blocks are say (the input), opts (the
// select and the checkboxes), preview, copy and code.
func SurfStretchy() map[string]any {
	tile := func(n string) map[string]any { return map[string]any{"type": "emoji", "name": n} }
	word := []any{tile("stretchy-l")}
	for range 4 {
		word = append(word, tile("stretchy-m"))
	}
	word = append(word, tile("stretchy-r"))
	opt := func(t, v string) map[string]any { return map[string]any{"text": pt(t), "value": v} }
	return surfView(SurfStretchyView, SurfStretchyView, "", "Stretchy text", "", "Done", "st1", []any{
		map[string]any{"type": "input", "block_id": "say", "label": pt("What should it say?"),
			"element": map[string]any{"type": "plain_text_input", "action_id": "v", "multiline": true, "initial_value": "hi"}},
		map[string]any{"type": "actions", "block_id": "opts", "elements": []any{
			map[string]any{"type": "static_select", "action_id": "style", "initial_option": opt("stretchy", "stretchy"),
				"options": []any{opt("stretchy", "stretchy"), opt("blocky", "blocky")}},
			map[string]any{"type": "checkboxes", "action_id": "flags",
				"options": []any{opt("Give every tile a face", "face"), opt("Tight spacing (tiles touch)", "tight")}},
		}},
		map[string]any{"type": "rich_text", "block_id": "preview", "elements": []any{
			map[string]any{"type": "rich_text_section", "elements": word}}},
		map[string]any{"type": "actions", "block_id": "copy", "elements": []any{
			map[string]any{"type": "button", "action_id": "copy", "style": "primary", "text": pt("Copy"), "value": "copy"}}},
		map[string]any{"type": "rich_text", "block_id": "code", "elements": []any{
			map[string]any{"type": "rich_text_preformatted", "elements": []any{map[string]any{"type": "text", "text": StretchyCode}}}}},
	})
}

// stretchImage serves /img/<w>x<h>/stretch-<tile>.png: a transparent
// tile with StretchyBar across it from 3/8 to 5/8 of its height, edge to
// edge for the middle tile, from a quarter in for the left end and to a
// quarter short of the edge for the right one, so tiles drawn side by side
// make one unbroken bar.
func stretchImage(path string) ([]byte, string, bool) {
	dims, name, ok := strings.Cut(path, "/")
	tile, isTile := strings.CutPrefix(name, "stretch-")
	if !ok || !isTile || !strings.HasSuffix(tile, ".png") {
		return nil, "", false
	}
	var w, h int
	if n, _ := fmt.Sscanf(dims, "%dx%d", &w, &h); n != 2 || w <= 0 || h <= 0 || w > 512 || h > 512 {
		return nil, "", false
	}
	x0, x1 := 0, w
	switch strings.TrimSuffix(tile, ".png") {
	case "stretchy-l":
		x0 = w / 4
	case "stretchy-r":
		x1 = w - w/4
	case "stretchy-m":
	default:
		return nil, "", false
	}
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := h * 3 / 8; y < h*5/8; y++ {
		for x := x0; x < x1; x++ {
			im.SetNRGBA(x, y, StretchyBar)
		}
	}
	var buf bytes.Buffer
	_ = (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, im)
	return buf.Bytes(), "image/png", true
}
