package richview

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/richtext"
)

// Media draws a message's playable videos: Slack's VideoPlayer, whose
// VideoControlsOverlay is there from the start and whose <video> comes
// with the first click (fIJO; internal/ui/videoplayer). With
// Options.Media set, every video tile Media takes (MediaTile) becomes a
// slot: the poster stays in the doc's recording, and Media draws over it
// on every layout (the overlay, the video's frames once it plays) and
// takes the tile's input, so a click on the tile is the player's, not
// Handler.OpenFile. richview, and pixeltest, which builds on stock Gio,
// never import internal/media: this interface is all they see of it.
type Media interface {
	// MediaTile reports whether Media draws f's tile, one of the files
	// tiles a gallery draws (overflow: the one carrying "+ N").
	MediaTile(f *richtext.File, files int, overflow bool) bool
	// LayoutMedia draws s in gtx.Constraints (exactly the tile).
	LayoutMedia(gtx layout.Context, s MediaSlot) layout.Dimensions
}

// MediaSlot is a video tile Media draws, in the tile's coordinates.
type MediaSlot struct {
	File *richtext.File
	// At is the tile's top-left in the View (Handler anchors are View
	// coordinates).
	At image.Point
	// Poster is where the poster sits in the tile (contained at its own
	// size in a lone or paired tile, else covering it): the video draws
	// there, with Radii's corners.
	Poster image.Rectangle
	Radii  [4]int
	// Files are the tiles the gallery draws (Slack's
	// renderedFilesDimensions); Overflow, whether this one carries the
	// "+ N" of the files that don't fit (hasOverflowOverlay).
	Files    int
	Overflow bool
	// Radius, Border and BorderW are the tile's corners and its inset
	// outline (otl-ter), which Slack draws over the video too.
	Radius  int
	Border  color.NRGBA
	BorderW int
	// Mini is an audio file's player in its mini style (audioPlayerStyle
	// {style: "default", mini: true}, a pending file's in the composer:
	// PendingFile's FileAudio): the play button, the scrubber and the time,
	// with no speed control, transcript button or More actions, at the
	// natural rate (ignoresPlaybackSpeedPref).
	Mini bool
}

// mediaSlot is a tile Options.Media draws: r in the View's coordinates.
type mediaSlot struct {
	r image.Rectangle
	s MediaSlot
}

// mediaTile reserves gallery tile r (the poster at ir) for Options.Media,
// when it takes f. n and overflow are the gallery's.
func (l *lay) mediaTile(f *richtext.File, r, ir image.Rectangle, radii [4]int, n int, overflow bool) bool {
	m := l.opts.Media
	if m == nil || f == nil || !f.IsVideo || !m.MediaTile(f, n, overflow) {
		return false
	}
	if ir.Empty() {
		ir = r
	}
	l.d.media = append(l.d.media, mediaSlot{r: r, s: MediaSlot{File: f, At: r.Min, Poster: ir.Sub(r.Min), Radii: radii, Files: n,
		Overflow: overflow, Radius: l.dp(8), Border: l.st.Palette.Border, BorderW: l.dp(1)}})
	return true
}

// layoutMedia draws the media slots (Options.Media), over everything the
// doc drew and above its input areas.
func (v *View) layoutMedia(gtx layout.Context) {
	m := v.opts.Media
	if m == nil || len(v.d.media) == 0 {
		return
	}
	var opac paint.OpacityStack
	if v.opts.Dim {
		opac = paint.PushOpacity(gtx.Ops, 0.5)
	}
	for i := range v.d.media {
		s := &v.d.media[i]
		off := op.Offset(s.r.Min).Push(gtx.Ops)
		g := gtx
		g.Constraints = layout.Exact(s.r.Size())
		m.LayoutMedia(g, s.s)
		off.Pop()
	}
	if v.opts.Dim {
		opac.Pop()
	}
}

// InMedia reports whether pt (View coordinates) is on a tile
// Options.Media draws: its presses are the player's (the list's text
// selection leaves them alone).
func (v *View) InMedia(pt image.Point) bool { return v.inMedia(pt) }

// inMedia reports whether p is on a tile Options.Media draws.
func (v *View) inMedia(pt image.Point) bool {
	for i := range v.d.media {
		if pt.In(v.d.media[i].r) {
			return true
		}
	}
	return false
}

// MediaRect is a video tile Options.Media draws, as the last layout
// placed it (View coordinates).
type MediaRect struct {
	File *richtext.File
	R    image.Rectangle
}

// MediaRects lists the tiles Options.Media draws, for the owner's
// visibility (the share of a playing video still on screen).
func (v *View) MediaRects() []MediaRect {
	if len(v.d.media) == 0 {
		return nil
	}
	out := make([]MediaRect, len(v.d.media))
	for i := range v.d.media {
		out[i] = MediaRect{File: v.d.media[i].s.File, R: v.d.media[i].r}
	}
	return out
}
