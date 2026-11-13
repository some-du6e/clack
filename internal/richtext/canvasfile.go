package richtext

import "github.com/some-du6e/clack/internal/slack"

// A canvas shared as a message's file (Slack's MessageFile for a quip
// file, boot-shared Zw0E): the pillow card's header (MessageFileHeader:
// the canvas tile, CanvasFileName's title, "Canvas") and, under it,
// MessageFilePreview's FileQuip (QrUf): the canvas's opening sections
// as Block Kit (useQuipBlocks: the canvas's Block Kit thumbnail, its
// first 11 blocks, the title's header dropped with hideTitle). Slack
// reads that thumbnail through Quip's own client (the thread's
// blockKitThumbnail); Clack builds the same blocks from the canvas's
// HTML export (core canvas_refs.go), in memory only.

// CanvasPreviewer is an optional Resolver extension (store.Store): a
// canvas's preview blocks; ok false while not fetched (the resolver asks
// for it and the Doc is rebuilt).
type CanvasPreviewer interface {
	CanvasPreview(fileID string) (blocks []Block, ok bool)
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
		out.CanvasPreview, _ = cp.CanvasPreview(f.ID)
	}
}
