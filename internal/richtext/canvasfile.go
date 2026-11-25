package richtext

import (
	"encoding/json"

	"github.com/some-du6e/clack/internal/slack"
)

// A canvas shared as a message's file (Slack's MessageFile for a quip
// file, boot-shared Zw0E): the pillow card's header (MessageFileHeader:
// the canvas tile, CanvasFileName's title, "Canvas") and, under it,
// MessageFilePreview's FileQuip (6WLC, with hideTitle): the canvas's
// Block Kit thumbnail as useQuipBlocks cuts it (build d4845cd3e2ea).
// Slack reads that thumbnail through Quip (the thread's
// getBlockKitThumbnailValue: its BLOCK_KIT_THUMBNAIL parcel), and so does
// Clack (core canvas_refs.go, quip/thumbnail.go), in memory only. Its
// mentions, dates and emoji are Block Kit's, drawn as a message's are.

// CanvasPreviewer is an optional Resolver extension (store.Store): a
// canvas's Block Kit thumbnail (nil: the canvas has none), ok when it was
// read since the file's updated time; ok false while no current one is
// held, when the card asks for it once it is on screen
// (File.CanvasPreviewWanted) and draws the last one read, if any, until it
// comes.
type CanvasPreviewer interface {
	CanvasPreview(fileID string, updated int64) (thumb json.RawMessage, ok bool)
}

// IsCanvasFile is Slack's isCanvas (2HRR DQ) for a message file.
func IsCanvasFile(f *File) bool {
	return f != nil && (f.Mode == "quip" || f.Type == "quip" || f.Type == "canvas")
}

func (c *builder) canvasFile(f *slack.File, out *File) {
	if !IsCanvasFile(out) {
		return
	}
	if bs := c.blocks(parseBlocks(f.TitleBlocks)); len(bs) > 0 {
		out.TitleSpans = firstLine(flatten(bs))
	}
	if len(out.TitleSpans) == 0 && f.Title != "" {
		out.TitleSpans = titleSpans(c.r, f.Title)
	}
	if cp, ok := c.r.(CanvasPreviewer); ok && f.ID != "" {
		// a thumbnail read before the canvas's last edit is drawn until the
		// new one comes (the card keeps its preview, not its place empty)
		thumb, ok := cp.CanvasPreview(f.ID, f.Updated)
		if !ok {
			out.CanvasPreviewWanted, out.CanvasUpdated = true, f.Updated
		}
		if thumb != nil {
			out.CanvasPreview = c.quipBlocks(thumb)
		}
	}
}

// quipBlocks is useQuipBlocks for a message card (no section,
// hideTitle, no hideHeaderImage): nothing for a thumbnail that errored or
// has no blocks, else its first 11 blocks without the first header,
// converted as a message's Block Kit. A canvas_header_image (Slack keeps
// it; its shape isn't known here) is left out.
func (c *builder) quipBlocks(thumb json.RawMessage) []Block {
	var t struct {
		Blocks []json.RawMessage `json:"blocks"`
		Error  bool              `json:"error"`
	}
	if json.Unmarshal(thumb, &t) != nil || t.Error || t.Blocks == nil {
		return nil
	}
	bs := t.Blocks[:min(len(t.Blocks), quipPreviewMax)]
	header := -1
	var keep []json.RawMessage
	for i, b := range bs {
		var h struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(b, &h)
		if h.Type == "header" && header < 0 {
			header = i // hideTitle: the first header goes
			continue
		}
		if h.Type == "canvas_header_image" {
			continue
		}
		keep = append(keep, b)
	}
	raw, err := json.Marshal(keep)
	if err != nil || len(keep) == 0 {
		return nil
	}
	return c.blocks(parseBlocks(raw))
}

// quipPreviewMax is useQuipBlocks' slice: 11 blocks, the title's header
// among them.
const quipPreviewMax = 11
