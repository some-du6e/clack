// Package icons is yoloslack's vector icon set: line icons on a 20×20 grid,
// drawn with clip.Path and modelled on the Slack desktop client's glyphs
// (redrawn, not copied).
//
//	icons.Get("hash")(gtx, 18, col)
//	icons.Hash(gtx, 18, col)
package icons

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"sort"
	"sync"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Icon draws itself in a size×size square with color col.
type Icon func(gtx layout.Context, size unit.Dp, col color.NRGBA) layout.Dimensions

// part is a sub-path: filled when stroke == 0, otherwise stroked with that
// width in viewbox units; fill parts with a stroke are filled and have
// their edge stroked too (see fl).
type part struct {
	d      string
	stroke float32
	fill   bool
}

type def struct {
	parts []part
	once  sync.Once
	segs  [][]seg
}

const view = 20

var registry = map[string]*def{}

// s and f build stroked (1.5 units, Slack's own weight: its glyphs are
// 1.5-unit lines outlined into fills) and filled parts. Strokes are drawn
// theme.StrokeGamma pixels wider or thinner so Gio's linear-light edges
// carry Chrome's ink (see draw).
func s(d string) part             { return part{d: d, stroke: 1.5} }
func sw(w float32, d string) part { return part{d: d, stroke: w} }
func f(d string) part             { return part{d: d} }

// fl is a filled shape drawn the way the strokes are: d runs 0.72 units
// inside the shape's edge (the centreline of its outer 1.44 units, as the
// strokes' paths are the centrelines of theirs), and is filled and
// stroked 1.44 wide, so the edge gets StrokeGamma in both modes (a plain
// fill reads heavy light-on-dark) and necks thinner than 1.44 stay
// joined. flw is fl with the edge band w wide (d runs w/2 inside the
// edge): narrower bands keep sharp points (sparkles) that a 0.72 inset
// would round off. Bands under 1 unit double-cover the antialiased edge
// pixels at 1x (the fill's edge and the stroke's both land in them), so
// none is narrower.
func fl(d string) part             { return flw(flEdge, d) }
func flw(w float32, d string) part { return part{d: d, stroke: w, fill: true} }

const flEdge = 1.44

func reg(name string, parts ...part) { registry[name] = &def{parts: parts} }

// Get returns the named icon (a Clack name, or a Slack SvgIcon name the
// set maps), or a visible placeholder box for unknown names (so a typo
// shows up on screen instead of panicking). Unknown names are logged once
// and listed by Missing; TestIconRefsExist fails on any literal name in
// the code that would draw the box.
//
// Kept small enough to inline: an inlined d.draw method value stays on the
// caller's stack (row layouts call Get per frame; see messages'
// TestRowLayoutAllocs), so the miss path lives in getMissing.
func Get(name string) Icon {
	if d, ok := registry[name]; ok {
		return d.draw
	}
	return getMissing(name)
}

// getMissing resolves a name the registry lacks: a Slack SvgIcon name
// (alias or suffixed) draws its counterpart, anything else the crossed box,
// logged once.
//
//go:noinline
func getMissing(name string) Icon {
	if n, ok := slackLookup(name); ok {
		return registry[n].draw
	}
	noteMissing(name, "missing")
	return registry["missing"].draw
}

var missing struct {
	sync.Mutex
	names map[string]bool
}

// noteMissing records (and logs, once per name) an icon name drawn as the
// placeholder drawn.
func noteMissing(name, drawn string) {
	missing.Lock()
	defer missing.Unlock()
	if missing.names[name] {
		return
	}
	if missing.names == nil {
		missing.names = map[string]bool{}
	}
	missing.names[name] = true
	log.Printf("icons: no icon for %q, drawing %q", name, drawn)
}

// Missing lists the icon names (Clack or Slack) that were asked for and
// drawn as a placeholder, sorted. Tests use it to fail on icons a view or
// plugin names that the set lacks.
func Missing() []string {
	missing.Lock()
	defer missing.Unlock()
	out := make([]string, 0, len(missing.names))
	for n := range missing.names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Has reports whether name is registered.
func Has(name string) bool { _, ok := registry[name]; return ok }

// Names lists all registered icons, sorted.
func Names() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (d *def) draw(gtx layout.Context, size unit.Dp, col color.NRGBA) layout.Dimensions {
	d.once.Do(func() {
		d.segs = make([][]seg, len(d.parts))
		for i, p := range d.parts {
			d.segs[i] = parsePath(p.d)
		}
	})
	px := gtx.Dp(size)
	sc := float32(px) / view
	gam := theme.StrokeGamma(col)
	for i, p := range d.parts {
		var path theme.PathPen // cached by content: Gio tessellates each icon stroke once
		path.Begin(gtx.Ops)
		open := false
		for _, sg := range d.segs[i] {
			switch sg.op {
			case 'M':
				path.MoveTo(sg.p[0].Mul(sc))
				open = true
			case 'L':
				path.LineTo(sg.p[0].Mul(sc))
			case 'C':
				path.CubeTo(sg.p[0].Mul(sc), sg.p[1].Mul(sc), sg.p[2].Mul(sc))
			case 'Z':
				if open {
					path.Close()
				}
			}
		}
		spec := path.End()
		switch {
		case p.fill && p.stroke > 0:
			paint.FillShape(gtx.Ops, col, clip.Outline{Path: spec}.Op())
			w := max(p.stroke*sc+gam, p.stroke*sc/2)
			paint.FillShape(gtx.Ops, col, clip.Stroke{Path: spec, Width: w}.Op())
		case p.stroke > 0:
			w := max(p.stroke*sc+gam, p.stroke*sc/2)
			paint.FillShape(gtx.Ops, col, clip.Stroke{Path: spec, Width: w}.Op())
		default:
			paint.FillShape(gtx.Ops, col, clip.Outline{Path: spec}.Op())
			// Dark-on-light fills lose gam/2 px of ink per edge too; an
			// outline stroke gives it back. (Light-on-dark fills would need
			// an erosion Gio has no op for, so the set draws dots and small
			// marks as strokes instead.)
			if gam > 0 {
				paint.FillShape(gtx.Ops, col, clip.Stroke{Path: spec, Width: gam}.Op())
			}
		}
	}
	return layout.Dimensions{Size: image.Pt(px, px)}
}

// ---- generated shapes ----

func circle(cx, cy, r float32) string {
	return fmt.Sprintf("M%g %g A%g %g 0 1 1 %g %g A%g %g 0 1 1 %g %gZ", cx-r, cy, r, r, cx+r, cy, r, r, cx-r, cy)
}

// hole is circle wound the other way: a round hole in a filled subpath
// (fills are nonzero).
func hole(cx, cy, r float32) string {
	return fmt.Sprintf("M%g %g A%g %g 0 1 0 %g %g A%g %g 0 1 0 %g %gZ", cx-r, cy, r, r, cx+r, cy, r, r, cx-r, cy)
}

func rrect(x, y, w, h, r float32) string {
	return fmt.Sprintf("M%g %g H%g A%g %g 0 0 1 %g %g V%g A%g %g 0 0 1 %g %g H%g A%g %g 0 0 1 %g %g V%g A%g %g 0 0 1 %g %gZ",
		x+r, y, x+w-r, r, r, x+w, y+r, y+h-r, r, r, x+w-r, y+h, x+r, r, r, x, y+h-r, y+r, r, r, x+r, y)
}

// disc is a filled circle of radius r, drawn as fl draws fills: a circle
// filled and stroked with a band up to 1 unit wide, so its edge gets
// StrokeGamma like the lines. (A ring stroked onto itself down to radius
// 0 leaves its centre pixel short at 1x.)
func disc(cx, cy, r float32) part {
	band := min(1, 0.8*r)
	return flw(band, circle(cx, cy, r-band/2))
}

// Convenience accessors for the most common icons.
var (
	Hash    = lazy("hash")
	Lock    = lazy("lock")
	CaretDn = lazy("caret-down")
	CaretRt = lazy("caret-right")
	Chevron = lazy("chevron-down")
	Plus    = lazy("plus")
	Search  = lazy("search")
	Close   = lazy("close")
	More    = lazy("more")
	Kebab   = lazy("kebab")
)

// lazy defers the registry lookup to draw time (package vars initialize
// before init registers the icons).
func lazy(name string) Icon {
	return func(gtx layout.Context, size unit.Dp, col color.NRGBA) layout.Dimensions {
		return Get(name)(gtx, size, col)
	}
}

func init() {
	// placeholder for unknown names
	reg("missing", s(rrect(3, 3, 14, 14, 2)), s("M3 3 L17 17 M17 3 L3 17"))

	reg("history-back", s("M3.6 11 A6.5 6.5 0 1 0 5.2 5.6"), s("M2.8 3.6 L4.7 6.3 L7.6 5.2"), s("M10 6.5 V10 L12.5 12"))
	reg("find", s("M2.5 5.5 H9 M2.5 9.5 H6.5 M2.5 13.5 H7"), s(circle(12.75, 11.25, 3.5)), s("M15.3 13.8 L17.5 16"))
	reg("close-small", s("M6.5 6.5 L13.5 13.5 M13.5 6.5 L6.5 13.5"))
	reg("plus-small", s("M10 5.5 V14.5 M5.5 10 H14.5"))
	reg("plus-circle", s(circle(10, 10, 6.75)), s("M10 6.75 V13.25 M6.75 10 H13.25"))

	reg("unreads", s("M4 5.5 H11 M4 10 H16 M4 14.5 H16"), f(circle(15, 5.5, 2)))
	reg("dot", f(circle(10, 10, 3)))

	reg("reply", s("M8.25 5 L3.75 9.5 L8.25 14 M3.75 9.5 H11.75 A4.5 4.5 0 0 1 16.25 14 V16"))
	reg("link", s("M8.6 11.4 A3 3 0 0 0 12.8 11.4 L15.3 8.9 A3 3 0 0 0 11.1 4.7 L9.9 5.9"), s("M11.4 8.6 A3 3 0 0 0 7.2 8.6 L4.7 11.1 A3 3 0 0 0 8.9 15.3 L10.1 14.1"))
	reg("bookmark-add", s("M5.75 3.25 H10 M15.25 9 V16.75 L10 13.25 L4.75 16.75 V4.25 A1 1 0 0 1 5.75 3.25"), s("M15 1.75 V6.75 M12.5 4.25 H17.5"))

	// composer
	// The formatting bar, redrawn on the centerlines of Slack's own glyphs
	// (bold, italic, underline, strikethrough, paragraph, link,
	// numbered-list, bulleted-list, quote, code, code-block, divider):
	// 1.5-unit lines with round ends; the B's bowls and the italic stem are
	// heavier, the list digits lighter, as in Slack's.
	reg("bold", sw(2.4, "M5.25 9.3 V3.1 H10.6 A3.1 3.1 0 0 1 10.6 9.3 H5.25 Z M5.25 9.3 H11.22 A3.78 3.78 0 0 1 11.22 16.85 H5.25 Z"))
	reg("italic", s("M7.75 2.75 H15.25 M4.75 17.25 H12.25"), sw(1.95, "M11.45 2.75 L8.55 17.25"))
	reg("underline", s("M5.5 2.38 V10.38 A4.5 4.5 0 0 0 14.5 10.38 V2.38 M2.75 17.87 H17.25"))
	reg("strike", s("M2.75 10.25 H17.25"),
		s("M14.25 5.5 C13.9 4.2 12.8 3.3 11.5 3 C10.5 2.7 9.4 2.75 8.5 3 C6.6 3.5 5.5 5.3 5.8 7.1 C6.05 8.6 7.6 10.25 11 10.25"),
		s("M14 13.25 C14.35 14.05 14.25 14.95 13.8 15.65 C13.3 16.35 12.5 16.85 11.55 17.1 C10.35 17.4 9.2 17.3 8.1 17 C6.9 16.65 6.05 15.8 5.75 14.6"))
	reg("paragraph", s("M8 10.25 H7.25 C5 10.25 3.25 8.6 3.25 6.25 C3.25 3.9 5 2.25 7.25 2.25 M8 17.75 V2.25 H16.75 M13 2.25 V17.75"))
	// two open links, point-symmetric about the centre
	reg("link", s("M10 5 L11.775 3.275 A3.5 3.5 0 0 1 16.725 8.225 L13.543 11.407 A3.5 3.5 0 0 1 8.48 11.29 M10 15 L8.225 16.725 A3.5 3.5 0 0 1 3.275 11.775 L6.457 8.593 A3.5 3.5 0 0 1 11.52 8.71"))
	reg("list-ordered", s("M7.75 4.25 H17.75 M7.75 10.75 H17.75 M7.75 17.25 H17.75"),
		sw(1, "M2.1 3 L3.5 2.5 V6.5 M2 6.5 H5"), sw(1, "M2.4 14.6 A1.25 1.25 0 0 1 4.75 14.25 C4.75 15.1 3.2 16.5 2.5 17.5 H5"))
	reg("list-bullet", s("M7.75 3 H17.75 M7.75 10 H17.75 M7.75 17 H17.75"), disc(3, 3, 1), disc(3, 10, 1), disc(3, 17, 1))
	reg("quote", s("M2.75 2.75 V17.25 M6.75 3.75 H15.25 M6.75 10.25 H17.25 M6.75 16.25 H14.25"))
	reg("code", s("M11.84 3.93 L8.15 16.07 M4.95 6.77 L1.74 9.98 L4.95 13.2 M15.05 6.77 L18.26 9.98 L15.05 13.2"))
	// an open box with a slanted </> over its top-left corner
	reg("code-block", s("M8.5 2.5 L6 10 M3.25 12.75 V16.25 A1 1 0 0 0 4.25 17.25 H17.25 A1 1 0 0 0 18.25 16.25 V4.75 A1 1 0 0 0 17.25 3.75 H15.25 M11 4 L13 6.25 L11 8.5 M3.5 4 L1.5 6.25 L3.5 8.5"))
	reg("divider", s("M2.25 9.95 H17.75 M4.75 5.75 V3.25 H15.25 V5.75 M4.75 14.25 V16.75 H15.25 V14.25"))
}

// Circle helper for consumers that want a filled dot at an arbitrary place
// without a path (presence dots, badges). Center c, diameter d (px).
func Dot(gtx layout.Context, c f32.Point, d float32, col color.NRGBA) {
	r := image.Rect(int(c.X-d/2+.5), int(c.Y-d/2+.5), int(c.X+d/2+.5), int(c.Y+d/2+.5))
	paint.FillShape(gtx.Ops, col, clip.Ellipse(r).Op(gtx.Ops))
}
