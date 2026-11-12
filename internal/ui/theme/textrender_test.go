package theme

import (
	"encoding/json"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"golang.org/x/image/math/fixed"

	"github.com/some-du6e/clack/assets/fonts"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Text rendering against Chrome (branch text-render).
//
// TestTextRenderShots draws the cells below (Slack's message, sidebar and
// meta text in both modes, regular and bold, plus white-on-black and
// black-on-white calibration lines) through Gio's GPU renderer (Mesa
// llvmpipe on Linux, D3D11 on Windows) at several device scales, and
// writes into $CLACK_TEXT_RENDER_DIR:
//
//	cases.json            the cells (CSS px), colours and strings
//	page-<s>x.html        the same cells for Chrome/Edge, in the embedded
//	                      Lato (copied next to it as Lato-*.ttf)
//	page-<s>x-cdn.html    the same in Slack's own Lato (lato-*.woff2 from
//	                      Slack's CDN, fetched by the workflow)
//	gio-<variant>-<s>x.png  Gio's rendering of every cell, per variant
//	gio-info.json         GOOS and the framebuffer probe (trProbeSRGB)
//
// .github/scripts/text_render_compare.py then measures Gio against the
// browser screenshots of the same pages.

type trCell struct {
	Name   string   `json:"name"`
	Mode   string   `json:"mode"` // dark, light, cal
	Ctx    string   `json:"ctx"`  // body, sidebar, meta, white, black
	Size   float32  `json:"size"` // CSS px (= Sp)
	Weight int      `json:"weight"`
	Text   string   `json:"text"`
	FG     [4]uint8 `json:"fg"` // NRGBA as drawn (opaque: Clack flattens translucent text in sRGB)
	BG     [3]uint8 `json:"bg"`
	Y      int      `json:"y"` // cell top, CSS px; cells are trCellW×trCellH at x=0
}

const (
	trCellW, trCellH = 600, 36 // CSS px
	trTextX, trBase  = 12, 24  // pen start and baseline in the cell
)

var (
	trScales   = []float32{1, 1.25, 1.5, 2}
	trVariants = []string{"clack", "raw", "skia", "skia-emb"}
)

const (
	trBodyText    = "Hamburgefonstiv: quick brown fox jumps over the lazy dog 0123"
	trSidebarText = "general   design-team   random   announcements"
	trMetaText    = "10:42 AM   3 replies   Last reply today at 9:15 PM"
)

func trCells() []trCell {
	var cs []trCell
	add := func(mode, ctx string, size float32, w int, txt string, fg, bg color.NRGBA) {
		cs = append(cs, trCell{
			Name: fmt.Sprintf("%s-%s-%d", mode, ctx, w), Mode: mode, Ctx: ctx, Size: size, Weight: w, Text: txt,
			FG: [4]uint8{fg.R, fg.G, fg.B, fg.A}, BG: [3]uint8{bg.R, bg.G, bg.B}, Y: len(cs) * trCellH,
		})
	}
	for _, dark := range []bool{true, false} {
		mode, p := "light", LightPalette
		// richview's body Text and Text2 (timestamps) over its Panel
		body, meta, panel := rgb(0x1d1c1d), rgb(0x616061), rgb(0xffffff)
		if dark {
			mode, p = "dark", DarkPalette
			body, meta, panel = rgb(0xb9babd), rgb(0xababad), rgb(0x1a1d21)
		}
		sb := Over(p.SidebarBg, p.ChromeB) // the sidebar's base (App.sidebarBase, at the gradient's dark stop)
		add(mode, "body", 15, 400, trBodyText, body, panel)
		add(mode, "body", 15, 700, trBodyText, body, panel)
		add(mode, "sidebar", 13, 400, trSidebarText, Over(p.SidebarText, sb), sb) // App.sidebarPalette flattens it
		add(mode, "sidebar", 13, 700, trSidebarText, p.SidebarTextUnread, sb)
		add(mode, "meta", 12, 400, trMetaText, meta, panel)
		add(mode, "meta", 12, 700, trMetaText, meta, panel)
	}
	add("cal", "white", 15, 400, trBodyText, rgb(0xffffff), rgb(0x000000))
	add("cal", "black", 15, 400, trBodyText, rgb(0x000000), rgb(0xffffff))
	return cs
}

func trTag(s float32) string { return strconv.FormatFloat(float64(s), 'g', -1, 32) + "x" }

func trPx(v int, s float32) int { return int(math.Round(float64(float32(v) * s))) }

func trNRGBA(c [4]uint8) color.NRGBA { return color.NRGBA{R: c[0], G: c[1], B: c[2], A: c[3]} }

func TestTextRenderShots(t *testing.T) {
	dir := os.Getenv("CLACK_TEXT_RENDER_DIR")
	if dir == "" {
		t.Skip("CLACK_TEXT_RENDER_DIR not set (font-shots' text-render jobs set it)")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cs := trCells()
	trWriteJSON(t, filepath.Join(dir, "cases.json"), map[string]any{
		"cell_w": trCellW, "cell_h": trCellH, "text_x": trTextX, "base": trBase,
		"scales": trScales, "variants": trVariants, "cells": cs,
	})
	for name, b := range map[string][]byte{"Lato-Regular.ttf": fonts.Regular, "Lato-Bold.ttf": fonts.Bold} {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	asc := trAscent(t)
	for _, s := range trScales {
		for _, cdn := range []bool{false, true} {
			name := "page-" + trTag(s) + ".html"
			if cdn {
				name = "page-" + trTag(s) + "-cdn.html"
			}
			if err := os.WriteFile(filepath.Join(dir, name), []byte(trPage(cs, s, asc, cdn)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	probe := trProbeSRGB(t)
	trWriteJSON(t, filepath.Join(dir, "gio-info.json"), map[string]any{"goos": runtime.GOOS, "goarch": runtime.GOARCH, "srgb_probe": probe})
	// Half-alpha white over black: 188 when the target blends in linear
	// light (sRGB framebuffer), 128 when it doesn't.
	if probe.R < 180 {
		t.Errorf("half-alpha white over black = %d, want ≈188 (linear-light blending into an sRGB target)", probe.R)
	}
	th := New(false)
	for _, s := range trScales {
		for _, v := range trVariants {
			img := trRender(t, th, cs, s, v)
			f, err := os.Create(filepath.Join(dir, "gio-"+v+"-"+trTag(s)+".png"))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, img); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
	}
}

func trWriteJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// trAscent is the embedded Lato's ascender over its em (hhea, = OS/2 typo:
// USE_TYPO_METRICS is set), which Blink rounds to whole device pixels for
// the baseline of a line-height:normal line.
func trAscent(t *testing.T) float64 {
	f := parseFace(fonts.Regular).Face()
	ext, ok := f.FontHExtents()
	if !ok {
		t.Fatal("Lato: no horizontal extents")
	}
	return float64(ext.Ascender) / float64(f.Upem())
}

// trPage is cs as HTML for Chrome at device scale s: each cell a box in its
// background, the text's baseline on the same device row as Gio's.
func trPage(cs []trCell, s float32, asc float64, cdn bool) string {
	fam, reg, bold := "ClackLato", "Lato-Regular.ttf", "Lato-Bold.ttf"
	if cdn {
		fam, reg, bold = "SlackCDNLato", "lato-regular-ef8e44a.woff2", "lato-bold-78da5d4.woff2"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<!doctype html><html><head><meta charset=utf-8><style>\n")
	fmt.Fprintf(&b, "@font-face{font-family:%s;font-weight:400;font-style:normal;font-display:block;src:url(%s)}\n", fam, reg)
	fmt.Fprintf(&b, "@font-face{font-family:%s;font-weight:700;font-style:normal;font-display:block;src:url(%s)}\n", fam, bold)
	fmt.Fprintf(&b, "html,body{margin:0;padding:0;background:#fff;width:%dpx;height:%dpx;overflow:hidden}\n", trCellW, len(cs)*trCellH)
	fmt.Fprintf(&b, "div{position:absolute;margin:0;padding:0;white-space:pre}\n</style></head><body>\n")
	for _, c := range cs {
		fmt.Fprintf(&b, "<div style=\"left:0;top:%dpx;width:%dpx;height:%dpx;background:rgb(%d,%d,%d)\"></div>\n",
			c.Y, trCellW, trCellH, c.BG[0], c.BG[1], c.BG[2])
		// Blink: baseline = top + round(ascent) in device pixels at a
		// line-height:normal line (Lato has no line gap).
		baseDev := float64(c.Y+trBase) * float64(s)
		top := (baseDev - math.Round(float64(s)*float64(c.Size)*asc)) / float64(s)
		fmt.Fprintf(&b, "<div style=\"left:%dpx;top:%.4fpx;font:%d %gpx/normal %s;color:rgba(%d,%d,%d,%.4f)\">%s</div>\n",
			trTextX, top, c.Weight, c.Size, fam, c.FG[0], c.FG[1], c.FG[2], float64(c.FG[3])/255, html.EscapeString(c.Text))
	}
	b.WriteString("</body></html>\n")
	return b.String()
}

// trProbeSRGB paints half-alpha white over black through the GPU renderer.
func trProbeSRGB(t *testing.T) color.RGBA {
	var ops op.Ops
	r := clip.Rect{Max: image.Pt(8, 8)}
	paint.FillShape(&ops, color.NRGBA{A: 0xff}, r.Op())
	paint.FillShape(&ops, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x80}, r.Op())
	return pixeltest.Render(t, 8, 8, &ops).RGBAAt(4, 4)
}

func trRender(t *testing.T, th *Theme, cs []trCell, s float32, variant string) *image.RGBA {
	w, h := trPx(trCellW, s), trPx(len(cs)*trCellH, s)
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: s, PxPerSp: s}, Constraints: layout.Exact(image.Pt(w, h))}
	for _, c := range cs {
		th.SetDark(c.Mode == "dark")
		th.Chrome = c.Ctx == "sidebar" // the sidebar draws in the chrome scope (App.Layout)
		y0 := trPx(c.Y, s)
		paint.FillShape(&ops, trNRGBA([4]uint8{c.BG[0], c.BG[1], c.BG[2], 0xff}), clip.Rect(image.Rect(0, y0, w, y0+trPx(trCellH, s))).Op())
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(w-trPx(trTextX, s), 4*trPx(trCellH, s))}
		m := op.Record(&ops)
		d := trDraw(g, th, c, variant, s)
		call := m.Stop()
		top := y0 + trPx(trBase, s) - (d.Size.Y - d.Baseline)
		st := op.Offset(image.Pt(trPx(trTextX, s), top)).Push(&ops)
		call.Add(&ops)
		st.Pop()
	}
	th.Chrome = false
	return pixeltest.Render(t, w, h, &ops)
}

func trDraw(gtx layout.Context, th *Theme, c trCell, variant string, s float32) layout.Dimensions {
	w := Regular
	if c.Weight >= 700 {
		w = Bold
	}
	o := LabelOpts{Font: Font(w), Size: unit.Sp(c.Size), MaxLines: 1, Color: trNRGBA(c.FG)}
	k := trSkiaContrast(o.Color, 1.0) // Chromium on Windows: SK_GAMMA_CONTRAST=1.0, SK_GAMMA_SRGB
	switch variant {
	case "clack": // as shipped
		if c.Ctx == "body" {
			// richview (messages.styleForTheme): light mode strokes every
			// body glyph by BodyEmboldenFor(Fg); dark mode adds
			// LightEmbolden·(1-1/scale) to light glyphs; no fade.
			// drawText scales by weightEmbolden, richview doesn't.
			emb := BodyEmboldenFor(LightPalette.Fg)
			if c.Mode == "dark" {
				emb = 0
				if y := (0.299*float32(o.Color.R) + 0.587*float32(o.Color.G) + 0.114*float32(o.Color.B)) / 255; y >= 0.6 && s > 1 {
					emb = 0.34 * (1 - 1/s)
				}
			}
			return drawText(gtx, th.Shaper, o, c.Text, emb/weightEmbolden(o.Font.Weight))
		}
		return th.Text(gtx, o, c.Text)
	case "raw": // Gio's coverage, linear-light blend, nothing added
		return trPaint(gtx, th.Shaper, o, c.Text, 0, 0)
	case "skia": // raw plus Skia's preblend contrast, as a second paint
		return trPaint(gtx, th.Shaper, o, c.Text, 0, k)
	case "skia-emb": // raw grown by the contrast's ink (k/6 px per edge)
		return trPaint(gtx, th.Shaper, o, c.Text, k/3, 0)
	}
	panic("variant " + variant)
}

// trPaint lays txt out like drawText and fills its outline (grown by emb
// px) in o.Color; k > 0 paints the same coverage again at alpha k·α, so
// the coverage becomes a + k·a·(1-a) (the second paint blends over the
// first in linear light).
func trPaint(gtx layout.Context, sh *text.Shaper, o LabelOpts, txt string, emb, k float32) layout.Dimensions {
	m := op.Record(gtx.Ops)
	d := drawText(gtx, sh, o, txt, 0) // placement (dims, baseline)
	m.Stop()
	cs := gtx.Constraints
	sh.LayoutString(text.Parameters{
		Font: o.Font, PxPerEm: fixed.I(gtx.Sp(o.Size)), MaxLines: o.MaxLines, Truncator: "…",
		Alignment: o.Align, WrapPolicy: o.Wrap, MaxWidth: cs.Max.X, MinWidth: cs.Min.X,
		Locale: gtx.Locale, LineHeight: fixed.I(gtx.Sp(o.LineHeight)),
	}, txt)
	var line []text.Glyph
	for g, ok := sh.NextGlyph(); ok; g, ok = sh.NextGlyph() {
		line = append(line, g)
	}
	if len(line) == 0 {
		return d
	}
	t := op.Affine(f32.AffineId().Offset(f32.Pt(float32(line[0].X)/64, float32(line[0].Y)))).Push(gtx.Ops)
	path, ok := EmboldenedShape(sh, line, emb)
	if !ok {
		path = sh.Shape(line)
	}
	cl := clip.Outline{Path: path}.Op().Push(gtx.Ops)
	paint.ColorOp{Color: o.Color}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	if k > 0 {
		c2 := o.Color
		c2.A = uint8(float32(c2.A)*k + 0.5)
		paint.ColorOp{Color: c2}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
	}
	cl.Pop()
	t.Pop()
	return d
}

// trSkiaContrast is the contrast Skia's mask gamma adds to an A8 glyph
// mask drawn in c (SkMaskGamma.cpp SkTMaskGamma_build_correcting_lut:
// adjustedContrast = contrast·toLuma(1-src)), src being c's luminance as
// SkScalerContext reduces it: each channel to 3 bits
// (SkTMaskGamma::CanonicalColor, SkScalerContextRec::setLuminanceColor),
// SkComputeLuminance (r·54+g·183+b·19)>>8 for A8 (PreprocessRec), 3 bits
// again. Gamma 0 is sRGB (SK_GAMMA_EXPONENT's default, SkTypes.h).
func trSkiaContrast(c color.NRGBA, contrast float64) float32 {
	cn := func(v uint8) uint8 { b := v >> 5 << 5; return b | b>>3 | b>>6 } // sk_t_scale255<3>
	lum := (int(cn(c.R))*54 + int(cn(c.G))*183 + int(cn(c.B))*19) >> 8
	src := float64(cn(uint8(lum))) / 255
	return float32(contrast * trLin(1-src))
}

func trLin(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}
