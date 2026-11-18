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
		bs, _ := cp.CanvasPreview(f.ID)
		out.CanvasPreview = namedPreview(c.r, bs)
	}
}

// namedPreview is a canvas preview with its mentions named through r on
// every conversion: Slack's FileQuip renders the thumbnail's Block Kit with
// the message renderer (BaseMrkdwnChannel, MemberSlug, UserGroup), which
// reads each mention's model by id, and Quip's export gives a mention only
// its id (canvas parse.go). The store's blocks are left as they are.
func namedPreview(r Resolver, bs []Block) []Block {
	var out []Block
	for i := range bs {
		b := bs[i]
		sp, named := namedSpans(r, b.Spans)
		var items [][]Span
		for j, it := range b.Items {
			if n, ok := namedSpans(r, it); ok {
				if items == nil {
					items = append([][]Span(nil), b.Items...)
				}
				items[j] = n
			}
		}
		if !named && items == nil {
			if out != nil {
				out = append(out, b)
			}
			continue
		}
		if out == nil {
			out = append(make([]Block, 0, len(bs)), bs[:i]...)
		}
		b.Spans = sp
		if items != nil {
			b.Items = items
		}
		out = append(out, b)
	}
	if out == nil {
		return bs
	}
	return out
}

// namedSpans names sp's user, channel and group mentions (those with an
// id) as a message's are; ok false when there are none (sp returned).
func namedSpans(r Resolver, sp []Span) ([]Span, bool) {
	var out []Span
	for i, s := range sp {
		if s.ID == "" {
			continue
		}
		var n Span
		switch s.Kind {
		case Channel:
			n = channelSpan(r, s.ID, "")
		case User:
			n = userSpan(r, s.ID, "")
		case Usergroup:
			n = usergroupSpan(r, s.ID, "")
		default:
			continue
		}
		n.Style = s.Style
		if out == nil {
			out = append([]Span(nil), sp...)
		}
		out[i] = n
	}
	if out == nil {
		return sp, false
	}
	return out, true
}
