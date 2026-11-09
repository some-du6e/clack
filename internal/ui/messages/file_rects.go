package messages

import (
	"image"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/richview"
)

// FileRect is a file's clickable box on screen: a gallery tile (the "+ N"
// tile is the file under it) or a file card.
type FileRect struct {
	TS   string // the message
	File *richtext.File
	R    image.Rectangle // window coordinates
}

// FileRects lists the clickable files of the visible messages as of the
// last frame, in window coordinates (the same mapping the list's context
// menu uses for a press on a file).
func (v *ChannelView) FileRects() []FileRect { return v.s.fileRects() }

// FileRects is ChannelView.FileRects for the thread.
func (v *ThreadView) FileRects() []FileRect { return v.s.fileRects() }

func (s *listState) fileRects() []FileRect {
	var out []FileRect
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if s.items[i].kind != itMsg || i >= len(s.heights) || s.heights[i] <= 0 {
			continue
		}
		r := s.rows[s.items[i].key]
		if r == nil || r.rv == nil || r.edit != nil {
			continue
		}
		off := s.origin.Add(image.Pt(r.bodyRect.Min.X, s.pos[i]+r.bodyRect.Min.Y))
		for _, fr := range r.rv.FileRects() {
			out = append(out, FileRect{TS: r.src.TS, File: fr.File, R: fr.R.Add(off)})
		}
	}
	return out
}

// ImageRect is an image's clickable box on screen (an image block, an
// attachment image), in window coordinates.
type ImageRect struct {
	TS    string // the message
	Image *richtext.Image
	R     image.Rectangle
}

// ImageRects lists the boxes of the visible messages that open the image
// viewer, as of the last frame, in window coordinates.
func (v *ChannelView) ImageRects() []ImageRect { return v.s.imageRects() }

// ImageRects is ChannelView.ImageRects for the thread.
func (v *ThreadView) ImageRects() []ImageRect { return v.s.imageRects() }

func (s *listState) imageRects() []ImageRect {
	var out []ImageRect
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if s.items[i].kind != itMsg || i >= len(s.heights) || s.heights[i] <= 0 {
			continue
		}
		r := s.rows[s.items[i].key]
		if r == nil || r.rv == nil || r.edit != nil {
			continue
		}
		off := s.origin.Add(image.Pt(r.bodyRect.Min.X, s.pos[i]+r.bodyRect.Min.Y))
		for _, ir := range r.rv.ImageRects() {
			out = append(out, ImageRect{TS: r.src.TS, Image: ir.Image, R: ir.R.Add(off)})
		}
	}
	return out
}

// LinkRect is a link's box on screen (a URL, a mention, a channel), in
// window coordinates.
type LinkRect struct {
	TS string // the message
	richview.LinkRect
}

// LinkRects lists the links of the visible messages as of the last
// frame, in window coordinates.
func (v *ChannelView) LinkRects() []LinkRect { return v.s.linkRects() }

// LinkRects is ChannelView.LinkRects for the thread.
func (v *ThreadView) LinkRects() []LinkRect { return v.s.linkRects() }

func (s *listState) linkRects() []LinkRect {
	var out []LinkRect
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if s.items[i].kind != itMsg || i >= len(s.heights) || s.heights[i] <= 0 {
			continue
		}
		r := s.rows[s.items[i].key]
		if r == nil || r.rv == nil || r.edit != nil {
			continue
		}
		off := s.origin.Add(image.Pt(r.bodyRect.Min.X, s.pos[i]+r.bodyRect.Min.Y))
		for _, lr := range r.rv.LinkRects() {
			lr.R = lr.R.Add(off)
			out = append(out, LinkRect{TS: r.src.TS, LinkRect: lr})
		}
	}
	return out
}
