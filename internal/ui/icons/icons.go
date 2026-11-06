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
	"math"
	"sort"
	"strings"
	"sync"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
)

// Icon draws itself in a size×size square with color col.
type Icon func(gtx layout.Context, size unit.Dp, col color.NRGBA) layout.Dimensions

// part is a sub-path: filled when stroke == 0, otherwise stroked with that
// width in viewbox units.
type part struct {
	d      string
	stroke float32
}

type def struct {
	parts []part
	once  sync.Once
	segs  [][]seg
}

const view = 20

var registry = map[string]*def{}

// s and f build stroked (1.5 units) and filled parts.
// 1.75 (not Slack's 1.5): Gio blends in linear light, so thin antialiased
// strokes read lighter than Chrome's; measured against the real client.
func s(d string) part                { return part{d: d, stroke: 1.75} }
func sw(w float32, d string) part    { return part{d: d, stroke: w} }
func f(d string) part                { return part{d: d} }
func reg(name string, parts ...part) { registry[name] = &def{parts: parts} }

// Get returns the named icon, or a visible placeholder box for unknown names
// (so a typo shows up on screen instead of panicking).
func Get(name string) Icon {
	d, ok := registry[name]
	if !ok {
		d = registry["missing"]
	}
	return d.draw
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
	for i, p := range d.parts {
		var path clip.Path
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
		if p.stroke > 0 {
			paint.FillShape(gtx.Ops, col, clip.Stroke{Path: spec, Width: p.stroke * sc}.Op())
		} else {
			paint.FillShape(gtx.Ops, col, clip.Outline{Path: spec}.Op())
		}
	}
	return layout.Dimensions{Size: image.Pt(px, px)}
}

// ---- generated shapes ----

func circle(cx, cy, r float32) string {
	return fmt.Sprintf("M%g %g A%g %g 0 1 1 %g %g A%g %g 0 1 1 %g %gZ", cx-r, cy, r, r, cx+r, cy, r, r, cx-r, cy)
}

func rrect(x, y, w, h, r float32) string {
	return fmt.Sprintf("M%g %g H%g A%g %g 0 0 1 %g %g V%g A%g %g 0 0 1 %g %g H%g A%g %g 0 0 1 %g %g V%g A%g %g 0 0 1 %g %gZ",
		x+r, y, x+w-r, r, r, x+w, y+r, y+h-r, r, r, x+w-r, y+h, x+r, r, r, x, y+h-r, y+r, r, r, x+r, y)
}

func star(cx, cy, ro, ri float32) string {
	var b strings.Builder
	for i := 0; i < 10; i++ {
		r := ro
		if i%2 == 1 {
			r = ri
		}
		a := -math.Pi/2 + float64(i)*math.Pi/5
		x, y := cx+r*float32(math.Cos(a)), cy+r*float32(math.Sin(a))
		if i == 0 {
			fmt.Fprintf(&b, "M%.3f %.3f", x, y)
		} else {
			fmt.Fprintf(&b, "L%.3f %.3f", x, y)
		}
	}
	b.WriteString("Z")
	return b.String()
}

func gear(cx, cy, ro, ri float32, teeth int) string {
	var b strings.Builder
	n := teeth * 4
	for i := 0; i < n; i++ {
		r := ri
		if i%4 == 1 || i%4 == 2 {
			r = ro
		}
		a := float64(i)*2*math.Pi/float64(n) - math.Pi/float64(n)
		x, y := cx+r*float32(math.Cos(a)), cy+r*float32(math.Sin(a))
		if i == 0 {
			fmt.Fprintf(&b, "M%.3f %.3f", x, y)
		} else {
			fmt.Fprintf(&b, "L%.3f %.3f", x, y)
		}
	}
	b.WriteString("Z")
	return b.String()
}

func dots(pts ...float32) []part {
	var out []part
	for i := 0; i+1 < len(pts); i += 2 {
		out = append(out, f(circle(pts[i], pts[i+1], 1.5)))
	}
	return out
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

	// navigation
	reg("hash", s("M8.2 3 L6.6 17 M13.4 3 L11.8 17 M3.5 7.25 H16.5 M3 12.75 H16"))
	reg("lock", s(rrect(4.5, 8.75, 11, 8.5, 1.75)), s("M7 8.75 V6.5 A3 3 0 0 1 13 6.5 V8.75"))
	reg("lock-filled", f(rrect(4, 8, 12, 9.5, 2)), s("M7 8.5 V6.5 A3 3 0 0 1 13 6.5 V8.5"))
	reg("caret-down", f("M5.75 8 L10 12.5 L14.25 8 Z"))
	reg("caret-right", f("M8 5.75 L12.5 10 L8 14.25 Z"))
	reg("caret-up", f("M5.75 12 L10 7.5 L14.25 12 Z"))
	reg("chevron-down", s("M5.5 8 L10 12.5 L14.5 8"))
	reg("chevron-up", s("M5.5 12 L10 7.5 L14.5 12"))
	reg("chevron-right", s("M8 5.5 L12.5 10 L8 14.5"))
	reg("chevron-left", s("M12 5.5 L7.5 10 L12 14.5"))
	reg("back", s("M16 10 H4.5 M9.25 5.25 L4.5 10 L9.25 14.75"))
	reg("forward", s("M4 10 H15.5 M10.75 5.25 L15.5 10 L10.75 14.75"))
	reg("arrow-down", s("M10 3.5 V16 M5.25 11.25 L10 16 L14.75 11.25"))
	reg("arrow-up", s("M10 16.5 V4 M5.25 8.75 L10 4 L14.75 8.75"))
	reg("history", sw(1.7, circle(10, 10, 7)), sw(1.7, "M10 5.75 V10 L12.75 11.75"))
	reg("history-back", s("M3.6 11 A6.5 6.5 0 1 0 5.2 5.6"), s("M2.8 3.6 L4.7 6.3 L7.6 5.2"), s("M10 6.5 V10 L12.5 12"))
	reg("find", s("M2.5 5.5 H9 M2.5 9.5 H6.5 M2.5 13.5 H7"), s(circle(12.75, 11.25, 3.5)), s("M15.3 13.8 L17.5 16"))
	reg("clock", s(circle(10, 10, 6.75)), s("M10 6 V10 L12.75 11.75"))
	reg("help", s(circle(10, 10, 6.75)), s("M7.9 7.9 A2.15 2.15 0 1 1 11.2 9.7 C10.5 10.15 10 10.6 10 11.4 V11.75"), f(circle(10, 14, 0.95)))
	reg("info", s(circle(10, 10, 6.75)), s("M10 9.25 V14"), f(circle(10, 6.6, 0.95)))
	reg("close", s("M5.25 5.25 L14.75 14.75 M14.75 5.25 L5.25 14.75"))
	reg("close-small", s("M6.5 6.5 L13.5 13.5 M13.5 6.5 L6.5 13.5"))
	reg("plus", s("M10 4 V16 M4 10 H16"))
	reg("plus-small", s("M10 5.5 V14.5 M5.5 10 H14.5"))
	reg("minus", s("M4 10 H16"))
	reg("check", s("M4.5 10.5 L8.25 14 L15.5 6.25"))
	reg("check-circle", s(circle(10, 10, 6.75)), s("M6.9 10.2 L9.1 12.3 L13.1 7.8"))
	reg("x-circle", s(circle(10, 10, 6.75)), s("M7.5 7.5 L12.5 12.5 M12.5 7.5 L7.5 12.5"))
	reg("plus-circle", s(circle(10, 10, 6.75)), s("M10 6.75 V13.25 M6.75 10 H13.25"))
	reg("warning", s("M10 3.5 L17 16 H3 Z"), s("M10 8 V11.5"), f(circle(10, 13.75, 0.9)))
	reg("more", dots(4.5, 10, 10, 10, 15.5, 10)...)
	reg("kebab", dots(10, 4.5, 10, 10, 10, 15.5)...)
	reg("search", s(circle(8.75, 8.75, 5.25)), s("M12.6 12.6 L16.5 16.5"))
	reg("filter", s("M3.5 5.5 H16.5 M6 10 H14 M8.5 14.5 H11.5"))
	reg("sort", s("M6.5 4 V16 M3.5 13 L6.5 16 L9.5 13 M13.5 16 V4 M10.5 7 L13.5 4 L16.5 7"))
	reg("external", s("M11.25 3.75 H16.25 V8.75 M16.25 3.75 L9.25 10.75"), s("M14.25 11.5 V15.25 A1 1 0 0 1 13.25 16.25 H4.75 A1 1 0 0 1 3.75 15.25 V6.75 A1 1 0 0 1 4.75 5.75 H8.5"))
	reg("download", s("M10 3.5 V12.75 M6 8.75 L10 12.75 L14 8.75 M4 16.25 H16"))
	reg("upload", s("M10 13.5 V4 M6 8 L10 4 L14 8 M4 16.25 H16"))
	reg("refresh", s("M15.8 8.4 A6 6 0 1 0 16 11.5"), s("M16.25 4.25 V8.5 H12"))
	reg("sidebar", s(rrect(3, 4, 14, 12, 2)), s("M8 4 V16"))
	reg("panel-right", s(rrect(3, 4, 14, 12, 2)), s("M12 4 V16"))
	reg("fullscreen", s("M3.75 7.5 V3.75 H7.5 M12.5 3.75 H16.25 V7.5 M16.25 12.5 V16.25 H12.5 M7.5 16.25 H3.75 V12.5"))

	// tab rail
	reg("home", s("M3.75 8.6 L10 3.5 L16.25 8.6 V15.75 A1 1 0 0 1 15.25 16.75 H12 V12.25 H8 V16.75 H4.75 A1 1 0 0 1 3.75 15.75 Z"))
	reg("home-filled", f("M3 8.4 L10 2.7 L17 8.4 V15.9 A1.6 1.6 0 0 1 15.4 17.5 H12.5 V12.5 H7.5 V17.5 H4.6 A1.6 1.6 0 0 1 3 15.9 Z"))
	reg("dms", s("M3.25 5.5 A2 2 0 0 1 5.25 3.5 H11.75 A2 2 0 0 1 13.75 5.5 V9.25 A2 2 0 0 1 11.75 11.25 H7.5 L4.5 13.75 V11.1 A2 2 0 0 1 3.25 9.25 Z"),
		s("M13.75 7.25 H14.75 A2 2 0 0 1 16.75 9.25 V13 A2 2 0 0 1 15.5 14.85 V17.25 L12.75 15 H10.25 A2 2 0 0 1 8.25 13 V11.25"))
	reg("dms-filled", f("M2.5 5.4 A2.4 2.4 0 0 1 4.9 3 H12.1 A2.4 2.4 0 0 1 14.5 5.4 V9.6 A2.4 2.4 0 0 1 12.1 12 H7.7 L4 14.9 V11.8 A2.4 2.4 0 0 1 2.5 9.6 Z"),
		s("M14.5 7.25 H15 A2 2 0 0 1 17 9.25 V13 A2 2 0 0 1 15.75 14.85 V17.25 L13 15 H10.5 A2 2 0 0 1 8.5 13 V12"))
	reg("bell", s("M10 3.25 A4.75 4.75 0 0 0 5.25 8 V11.5 L3.75 14 H16.25 L14.75 11.5 V8 A4.75 4.75 0 0 0 10 3.25 Z"), s("M8 16.25 A2 2 0 0 0 12 16.25"))
	reg("bell-filled", f("M10 2.5 A5.4 5.4 0 0 0 4.6 7.9 V11.3 L3 14.2 A.6.6 0 0 0 3.5 15 H16.5 A.6.6 0 0 0 17 14.2 L15.4 11.3 V7.9 A5.4 5.4 0 0 0 10 2.5 Z"), s("M8 16.5 A2 2 0 0 0 12 16.5"))
	reg("bell-off", s("M7 4.2 A4.75 4.75 0 0 1 14.75 8 V11.5 L15.5 12.75 M5.25 7.9 V11.5 L3.75 14 H13.5"), s("M8 16.25 A2 2 0 0 0 12 16.25"), s("M3 3 L17 17"))
	reg("activity", registry["bell"].parts...)
	reg("activity-filled", registry["bell-filled"].parts...)
	reg("files", s("M5 3.25 H11.25 L15.5 7.5 V15.75 A1 1 0 0 1 14.5 16.75 H5 A1 1 0 0 1 4 15.75 V4.25 A1 1 0 0 1 5 3.25 Z"), s("M11.25 3.25 V7.5 H15.5"))
	reg("files-filled", f("M4.9 2.5 H11 V6.5 A1.5 1.5 0 0 0 12.5 8 H16.25 V15.9 A1.6 1.6 0 0 1 14.65 17.5 H4.9 A1.6 1.6 0 0 1 3.3 15.9 V4.1 A1.6 1.6 0 0 1 4.9 2.5 Z"), f("M12.25 2.8 L16 6.6 H12.6 A.35.35 0 0 1 12.25 6.25 Z"))
	reg("later", s("M5.75 3.25 H14.25 A1 1 0 0 1 15.25 4.25 V16.75 L10 13.25 L4.75 16.75 V4.25 A1 1 0 0 1 5.75 3.25 Z"))
	reg("later-filled", f("M5.6 2.5 H14.4 A1.6 1.6 0 0 1 16 4.1 V17.1 A.5.5 0 0 1 15.2 17.5 L10 14 L4.8 17.5 A.5.5 0 0 1 4 17.1 V4.1 A1.6 1.6 0 0 1 5.6 2.5 Z"))
	reg("bookmark", registry["later"].parts...)
	reg("bookmark-filled", registry["later-filled"].parts...)
	reg("saved", registry["later"].parts...)
	reg("saved-filled", registry["later-filled"].parts...)
	reg("agents", s(circle(6.25, 6.25, 2.75)), s(circle(13.75, 6.25, 2.75)), s(circle(6.25, 13.75, 2.75)), s("M13.75 10.25 C14.1 12.3 14.95 13.15 17 13.5 C14.95 13.85 14.1 14.7 13.75 16.75 C13.4 14.7 12.55 13.85 10.5 13.5 C12.55 13.15 13.4 12.3 13.75 10.25 Z"))
	reg("apps", s(rrect(3.5, 3.5, 5, 5, 1.25)), s(rrect(11.5, 3.5, 5, 5, 1.25)), s(rrect(3.5, 11.5, 5, 5, 1.25)), s(rrect(11.5, 11.5, 5, 5, 1.25)))

	// sidebar
	reg("compose", s("M9.25 3.75 H5 A1.25 1.25 0 0 0 3.75 5 V15 A1.25 1.25 0 0 0 5 16.25 H15 A1.25 1.25 0 0 0 16.25 15 V10.75"), s("M14.6 3.1 L16.9 5.4 L10.4 11.9 L7.4 12.6 L8.1 9.6 Z"))
	reg("settings", s(gear(10, 10, 7, 5.6, 8)), s(circle(10, 10, 2.4)))
	reg("filter-sliders", s("M3.5 6 H9 M13 6 H16.5 M3.5 14 H6 M10 14 H16.5"), s(circle(11, 6, 2)), s(circle(8, 14, 2)))
	reg("threads", s("M10 3.25 A6.75 6.75 0 0 0 4.1 13.3 L3.25 16.75 L6.8 15.95 A6.75 6.75 0 1 0 10 3.25 Z"), s("M7 8.5 H13 M7 11.5 H11"))
	reg("huddle", s("M3.75 13 V10 A6.25 6.25 0 0 1 16.25 10 V13"), s(rrect(3.25, 11, 3.5, 6, 1.25)), s(rrect(13.25, 11, 3.5, 6, 1.25)))
	reg("headphones", registry["huddle"].parts...)
	reg("send", f("M2.9 3.1 A.7.7 0 0 1 3.9 2.4 L17.1 9.3 A.8.8 0 0 1 17.1 10.7 L3.9 17.6 A.7.7 0 0 1 2.9 16.9 L4.6 10.7 L11 10 L4.6 9.3 Z"))
	reg("drafts", s("M3.5 3.5 L16.75 10 L3.5 16.5 L5.4 10 Z"), s("M5.4 10 H10"))
	reg("directories", s(circle(7.5, 7, 2.75)), s("M2.75 16 A4.75 4.75 0 0 1 12.25 16"), s("M12.5 4.5 A2.6 2.6 0 0 1 12.5 9.5 M14.25 11.5 A4.6 4.6 0 0 1 17.25 16"))
	reg("people", registry["directories"].parts...)
	reg("user", s(circle(10, 7, 3.25)), s("M3.75 17 A6.25 5.5 0 0 1 16.25 17"))
	reg("user-plus", s(circle(8.5, 7, 3.25)), s("M2.5 17 A6 5.5 0 0 1 14.5 17"), s("M15.5 5 V10 M13 7.5 H18"))
	reg("channel-plus", s("M7.6 3 L6.2 15 M12.6 3 L11.6 11 M3.5 6.75 H15.5 M3 11.25 H11"), s("M15 12 V18 M12 15 H18"))
	reg("channels-section", s(rrect(3, 3, 14, 14, 3)), sw(1.3, "M8.6 6 L7.6 14 M12.4 6 L11.4 14 M6 8.4 H14 M5.7 11.6 H13.7"))
	reg("contact", s(rrect(4.25, 2.75, 11.5, 14.5, 2)), s(circle(10, 8.5, 2)), s("M6.9 14 A3.2 2.6 0 0 1 13.1 14"), s("M2.75 6 H4.25 M2.75 10 H4.25 M2.75 14 H4.25"))
	reg("star", s(star(10, 10.4, 7, 3.1)))
	reg("star-filled", f(star(10, 10.4, 7.4, 3.4)))
	reg("unreads", s("M4 5.5 H11 M4 10 H16 M4 14.5 H16"), f(circle(15, 5.5, 2)))
	reg("mention", s(circle(10, 10, 2.9)), s("M12.9 10 V11.1 A2.1 2.1 0 0 0 17.1 11.1 V10 A7.1 7.1 0 1 0 14.3 15.6"))
	reg("at", registry["mention"].parts...)
	reg("canvas", s(rrect(4, 3.25, 12, 13.5, 1.75)), s("M7 7.25 H13 M7 10 H13 M7 12.75 H10.5"))
	reg("list-view", s("M7 5 H16.5 M7 10 H16.5 M7 15 H16.5"), f(circle(3.75, 5, 1.1)), f(circle(3.75, 10, 1.1)), f(circle(3.75, 15, 1.1)))
	reg("workflow", s(rrect(3, 3.5, 6, 5, 1.25)), s(rrect(11, 11.5, 6, 5, 1.25)), s("M6 8.5 V11 A1.5 1.5 0 0 0 7.5 12.5 H11"))
	reg("lightning", s("M11.25 2.75 L4.75 11 H10 L8.75 17.25 L15.25 9 H10 Z"))
	reg("sun", s(circle(10, 10, 3.25)), s("M10 2.5 V4 M10 16 V17.5 M2.5 10 H4 M16 10 H17.5 M4.7 4.7 L5.75 5.75 M14.25 14.25 L15.3 15.3 M4.7 15.3 L5.75 14.25 M14.25 5.75 L15.3 4.7"))
	reg("moon", s("M15.9 12.4 A6.6 6.6 0 0 1 7.6 4.1 A6.6 6.6 0 1 0 15.9 12.4 Z"))
	reg("globe", s(circle(10, 10, 6.75)), s("M3.25 10 H16.75 M10 3.25 C7.9 5.4 7.9 14.6 10 16.75 M10 3.25 C12.1 5.4 12.1 14.6 10 16.75"))
	reg("eye", s("M2.5 10 C4.4 6.4 7 4.75 10 4.75 C13 4.75 15.6 6.4 17.5 10 C15.6 13.6 13 15.25 10 15.25 C7 15.25 4.4 13.6 2.5 10 Z"), s(circle(10, 10, 2.5)))
	reg("mail", s(rrect(3, 4.75, 14, 10.5, 1.75)), s("M3.5 5.75 L10 10.75 L16.5 5.75"))
	reg("phone", s("M6.2 3.5 L8 3.5 L9 7 L7.5 8.2 A9 9 0 0 0 11.8 12.5 L13 11 L16.5 12 V13.8 A1.7 1.7 0 0 1 14.8 15.5 A11.3 11.3 0 0 1 4.5 5.2 A1.7 1.7 0 0 1 6.2 3.5 Z"))
	reg("calendar", s(rrect(3.25, 4.5, 13.5, 12, 1.75)), s("M3.25 8.25 H16.75 M7 3 V6 M13 3 V6"))
	reg("image", s(rrect(3, 4, 14, 12, 1.75)), s("M3.5 14 L7.5 10 L10.5 13 L12.75 10.75 L16.5 14.25"), f(circle(12.75, 7.5, 1.3)))
	reg("play", f("M6.5 4.3 A.7.7 0 0 1 7.6 3.7 L15.7 9.4 A.7.7 0 0 1 15.7 10.6 L7.6 16.3 A.7.7 0 0 1 6.5 15.7 Z"))
	reg("pause", f(rrect(5, 4, 3.5, 12, 1)), f(rrect(11.5, 4, 3.5, 12, 1)))
	reg("volume", s("M3.5 7.75 H6.5 L10.25 4.5 V15.5 L6.5 12.25 H3.5 Z"), s("M13 7.25 A3.75 3.75 0 0 1 13 12.75 M15 5 A6.75 6.75 0 0 1 15 15"))
	reg("mute", s("M3.5 7.75 H6.5 L10.25 4.5 V15.5 L6.5 12.25 H3.5 Z"), s("M12.75 7.75 L17 12 M17 7.75 L12.75 12"))
	reg("sparkle", s("M10 3 C10.6 7 13 9.4 17 10 C13 10.6 10.6 13 10 17 C9.4 13 7 10.6 3 10 C7 9.4 9.4 7 10 3 Z"))
	reg("dot", f(circle(10, 10, 3)))
	reg("ring", s(circle(10, 10, 3.25)))

	// message actions
	reg("emoji", s(circle(10, 10, 6.75)), s("M7 11.75 A3.4 3.4 0 0 0 13 11.75"), f(circle(7.75, 8.25, 0.95)), f(circle(12.25, 8.25, 0.95)))
	reg("add-reaction", s("M16.6 9.1 A6.75 6.75 0 1 1 10.9 3.3"), s("M7 11.75 A3.4 3.4 0 0 0 13 11.75"), f(circle(7.75, 8.25, 0.95)), f(circle(12.25, 8.25, 0.95)), s("M15.25 1.75 V6.75 M12.75 4.25 H17.75"))
	reg("reply", s("M8.25 5 L3.75 9.5 L8.25 14 M3.75 9.5 H11.75 A4.5 4.5 0 0 1 16.25 14 V16"))
	reg("share", s("M11.75 5 L16.25 9.5 L11.75 14 M16.25 9.5 H8.25 A4.5 4.5 0 0 0 3.75 14 V16"))
	reg("forward-msg", registry["share"].parts...)
	reg("edit", s("M13.25 3.6 L16.4 6.75 L7.2 15.95 L3.6 16.4 L4.05 12.8 Z M11.5 5.35 L14.65 8.5"))
	reg("trash", s("M3.75 5.5 H16.25 M8 5.5 V4.25 A1 1 0 0 1 9 3.25 H11 A1 1 0 0 1 12 4.25 V5.5"), s("M5.25 5.5 L6.05 15.85 A1 1 0 0 0 7.05 16.75 H12.95 A1 1 0 0 0 13.95 15.85 L14.75 5.5"), s("M8.5 8.5 V13.75 M11.5 8.5 V13.75"))
	reg("pin", s("M11.9 2.9 L17.1 8.1 L15.6 8.6 L12.7 11.5 L12.6 14.9 L11.2 16.3 L3.7 8.8 L5.1 7.4 L8.5 7.3 L11.4 4.4 Z"), s("M7.2 12.8 L3 17"))
	reg("unpin", s("M11.9 2.9 L17.1 8.1 L15.6 8.6 L12.7 11.5 L12.6 14.9 L11.2 16.3 L3.7 8.8 L5.1 7.4 L8.5 7.3 L11.4 4.4 Z"), s("M7.2 12.8 L3 17"), s("M3 3 L17 17"))
	reg("copy", s(rrect(7, 7, 9.5, 9.5, 1.5)), s("M13 7 V4.75 A1.25 1.25 0 0 0 11.75 3.5 H4.75 A1.25 1.25 0 0 0 3.5 4.75 V11.75 A1.25 1.25 0 0 0 4.75 13 H7"))
	reg("link", s("M8.6 11.4 A3 3 0 0 0 12.8 11.4 L15.3 8.9 A3 3 0 0 0 11.1 4.7 L9.9 5.9"), s("M11.4 8.6 A3 3 0 0 0 7.2 8.6 L4.7 11.1 A3 3 0 0 0 8.9 15.3 L10.1 14.1"))
	reg("mark-unread", s("M16.5 10.5 V15 A1.5 1.5 0 0 1 15 16.5 H5 A1.5 1.5 0 0 1 3.5 15 V5 A1.5 1.5 0 0 1 5 3.5 H9.5"), f(circle(15, 5, 2.5)))
	reg("remind", s(circle(10, 10.75, 6)), s("M10 7.5 V10.75 L12.25 12.25 M3.5 4.75 L5.75 2.75 M16.5 4.75 L14.25 2.75"))
	reg("bookmark-add", s("M5.75 3.25 H10 M15.25 9 V16.75 L10 13.25 L4.75 16.75 V4.25 A1 1 0 0 1 5.75 3.25"), s("M15 1.75 V6.75 M12.5 4.25 H17.5"))

	// composer
	reg("bold", sw(1.9, "M6 3.9 H10.6 A3.05 3.05 0 0 1 10.6 10 H6 Z M6 10 H11.3 A3.05 3.05 0 0 1 11.3 16.1 H6 Z"))
	reg("italic", s("M8.75 4 H14 M6 16 H11.25 M11.6 4 L8.4 16"))
	reg("strike", s("M3.75 10 H16.25"), s("M13.4 6.3 C12.9 4.9 11.6 4 10 4 C8 4 6.6 5.1 6.6 6.6 C6.6 7.8 7.3 8.6 8.6 9.2"), s("M13.3 12.4 C13.5 12.8 13.6 13.2 13.6 13.6 C13.6 15.1 12.1 16 10 16 C8.1 16 6.7 15.2 6.3 13.8"))
	reg("list-bullet", s("M8 5.5 H16.25 M8 10 H16.25 M8 14.5 H16.25"), f(circle(4.5, 5.5, 1.2)), f(circle(4.5, 10, 1.2)), f(circle(4.5, 14.5, 1.2)))
	reg("list-ordered", s("M8.5 5.5 H16.25 M8.5 10 H16.25 M8.5 14.5 H16.25"), sw(1.1, "M3.6 3.9 L4.6 3.4 V7.1 M3.4 8.9 A1.1 1.1 0 0 1 5.5 9.3 C5.5 10 3.4 10.9 3.4 11.6 H5.6 M3.4 13 H5.5 L4.3 14.3 A1.1 1.1 0 1 1 3.4 16"))
	reg("quote", s("M4.5 4 V16"), s("M8 6 H16 M8 10 H16 M8 14 H13"))
	reg("code", s("M7 5.5 L2.75 10 L7 14.5 M13 5.5 L17.25 10 L13 14.5"))
	reg("code-block", s(rrect(2.75, 3.75, 14.5, 12.5, 1.75)), s("M8.25 7.75 L6 10 L8.25 12.25 M11.75 7.75 L14 10 L11.75 12.25"))
	reg("attach", s("M15.2 9.4 L9.7 14.9 A3.2 3.2 0 0 1 5.1 10.4 L10.9 4.6 A2.15 2.15 0 0 1 13.95 7.65 L8.3 13.3 A1.1 1.1 0 0 1 6.75 11.75 L11.9 6.6"))
	reg("mic", s("M7.5 4.75 A2.5 2.5 0 0 1 12.5 4.75 V9.75 A2.5 2.5 0 0 1 7.5 9.75 Z"), s("M4.5 9.25 A5.5 5.5 0 0 0 15.5 9.25 M10 14.75 V17.25"))
	reg("video", s("M2.75 6.25 A1.25 1.25 0 0 1 4 5 H11.75 A1.25 1.25 0 0 1 13 6.25 V13.75 A1.25 1.25 0 0 1 11.75 15 H4 A1.25 1.25 0 0 1 2.75 13.75 Z"), s("M13 8.75 L17.25 6.5 V13.5 L13 11.25"))
	reg("slash", s(rrect(3.25, 3.25, 13.5, 13.5, 3)), s("M11.75 6.25 L8.25 13.75"))
	reg("format", s("M2.75 15 L6.75 5 L10.75 15 M4.15 11.5 H9.35"), s(circle(15, 12.6, 2.15)), s("M17.15 10 V15"))
	reg("text-format", registry["format"].parts...)
	reg("shortcut", registry["lightning"].parts...)
	reg("expand", s("M11.5 3.75 H16.25 V8.5 M16.25 3.75 L11 9 M8.5 16.25 H3.75 V11.5 M3.75 16.25 L9 11"))
	reg("collapse", s("M16.25 8.5 H11.5 V3.75 M11.5 8.5 L16.75 3.25 M3.75 11.5 H8.5 V16.25 M8.5 11.5 L3.25 16.75"))
	reg("schedule", s(circle(10, 10, 6.75)), s("M10 6.25 V10 H13"))
}

// Circle helper for consumers that want a filled dot at an arbitrary place
// without a path (presence dots, badges). Center c, diameter d (px).
func Dot(gtx layout.Context, c f32.Point, d float32, col color.NRGBA) {
	r := image.Rect(int(c.X-d/2+.5), int(c.Y-d/2+.5), int(c.X+d/2+.5), int(c.Y+d/2+.5))
	paint.FillShape(gtx.Ops, col, clip.Ellipse(r).Op(gtx.Ops))
}
