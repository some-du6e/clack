package messages

import (
	"image"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
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
