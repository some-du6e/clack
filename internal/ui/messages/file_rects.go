package messages

import (
	"image"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
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

// FileActionRect is a shown FileActions button on screen (the hovered
// file's), in window coordinates.
type FileActionRect struct {
	TS string // the message
	richview.FileActionRect
}

// FileActionRects lists the buttons of the FileActions the pointer shows
// now, as of the last frame, in window coordinates.
func (v *ChannelView) FileActionRects() []FileActionRect { return v.s.fileActionRects() }

// FileActionRects is ChannelView.FileActionRects for the thread.
func (v *ThreadView) FileActionRects() []FileActionRect { return v.s.fileActionRects() }

func (s *listState) fileActionRects() []FileActionRect {
	var out []FileActionRect
	s.eachBody(func(ts string, off image.Point, rv *richview.View) {
		for _, r := range rv.FileActionRects() {
			r.R = r.R.Add(off)
			out = append(out, FileActionRect{TS: ts, FileActionRect: r})
		}
	})
	return out
}

// ReactionRect is a reaction pill on screen (its group's), in window
// coordinates.
type ReactionRect struct {
	TS   string // the message
	Name string // the group's emoji name
	R    image.Rectangle
}

// ReactionRects lists the reaction pills of the visible messages as of
// the last frame, in window coordinates.
func (v *ChannelView) ReactionRects() []ReactionRect { return v.s.reactionRects() }

func (s *listState) reactionRects() []ReactionRect {
	var out []ReactionRect
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if s.items[i].kind != itMsg || i >= len(s.heights) || s.heights[i] <= 0 {
			continue
		}
		r := s.rows[s.items[i].key]
		if r == nil {
			continue
		}
		off := s.origin.Add(image.Pt(0, s.pos[i]))
		for k := range r.reactRects {
			if k < len(r.groups) {
				out = append(out, ReactionRect{TS: r.src.TS, Name: r.groups[k].name, R: r.reactRects[k].Add(off)})
			}
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

// CardRect is a link preview card's box on screen (window coordinates).
type CardRect struct {
	TS string // the message
	richview.CardRect
}

// CardRects lists the visible messages' v3 link preview cards as of the
// last frame, in window coordinates.
func (v *ChannelView) CardRects() []CardRect {
	var out []CardRect
	v.s.eachBody(func(ts string, off image.Point, rv *richview.View) {
		for _, c := range rv.CardRects() {
			c.R = c.R.Add(off)
			out = append(out, CardRect{TS: ts, CardRect: c})
		}
	})
	return out
}

// WorkObjectRect is a work object card's box on screen (window
// coordinates).
type WorkObjectRect struct {
	TS string // the message
	richview.WorkObjectRect
}

// WorkObjectRects lists the visible messages' work object cards as of
// the last frame, in window coordinates.
func (v *ChannelView) WorkObjectRects() []WorkObjectRect {
	var out []WorkObjectRect
	v.s.eachBody(func(ts string, off image.Point, rv *richview.View) {
		for _, c := range rv.WorkObjectRects() {
			c.R = c.R.Add(off)
			out = append(out, WorkObjectRect{TS: ts, WorkObjectRect: c})
		}
	})
	return out
}

// RemoveRect is a link preview's remove button on screen.
type RemoveRect struct {
	TS string // the message
	richview.RemoveRect
}

// RemoveRects lists the visible messages' preview remove buttons as of
// the last frame, in window coordinates.
func (v *ChannelView) RemoveRects() []RemoveRect {
	var out []RemoveRect
	v.s.eachBody(func(ts string, off image.Point, rv *richview.View) {
		for _, c := range rv.RemoveRects() {
			c.R = c.R.Add(off)
			out = append(out, RemoveRect{TS: ts, RemoveRect: c})
		}
	})
	return out
}

// eachBody calls f with each visible message's body view and its window
// offset.
func (s *listState) eachBody(f func(ts string, off image.Point, rv *richview.View)) {
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if s.items[i].kind != itMsg || i >= len(s.heights) || s.heights[i] <= 0 {
			continue
		}
		r := s.rows[s.items[i].key]
		if r == nil || r.rv == nil || r.edit != nil {
			continue
		}
		f(r.src.TS, s.origin.Add(image.Pt(r.bodyRect.Min.X, s.pos[i]+r.bodyRect.Min.Y)), r.rv)
	}
}
