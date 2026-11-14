package icons

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// artRef names Slack's file for each illustration (art.go) and the size
// Slack draws it at.
var artRef = []struct {
	name, file string
	px         int
}{
	{"slack-logo-mark", "slack_logo_mark-8890ba0.svg", 20},
	{"focus-telescope", "focus-d8ca34d.svg", 64},
}

// The tolerances TestArtMatchesSlack holds each colour's shapes to: the
// coverage IoU and the covered area against Slack's file.
const (
	artIoU = 0.95
	artInk = 0.03
)

// TestArtMatchesSlack draws each illustration one colour at a time, black
// on white, through Gio's GPU renderer and compares its coverage with that
// colour's shapes in Slack's SVG file rasterized with exact area coverage
// (Chrome's), at the size Slack draws it and twice that: per colour the
// coverage IoU (Σmin/Σmax) at least artIoU, the covered area within
// artInk. CI downloads the files from the CDN into CLACK_SLACK_ART (the
// icon-shots workflow); ICON_SHOTS_DIR gets art-vs-slack-<name>-<px>.png,
// Slack's file composited the way Chrome does on the left and Clack's on
// the right, on white and on Slack's dark #1a1d21.
func TestArtMatchesSlack(t *testing.T) {
	dir := os.Getenv("CLACK_SLACK_ART")
	if dir == "" {
		t.Skip("CLACK_SLACK_ART (Slack's illustration SVGs) not set; the icon-shots workflow sets it")
	}
	shots := os.Getenv("ICON_SHOTS_DIR")
	for _, a := range artRef {
		raw, err := os.ReadFile(filepath.Join(dir, a.file))
		if err != nil {
			t.Fatal(err)
		}
		layers, view := svgLayers(t, string(raw))
		def := artRegistry[a.name]
		if def == nil {
			t.Errorf("%s: not registered", a.name)
			continue
		}
		if def.view != view {
			t.Errorf("%s: view %g, Slack's file %g", a.name, def.view, view)
		}
		fills := map[uint32]bool{}
		for _, p := range def.parts {
			fills[p.fill] = true
		}
		for _, l := range layers {
			if !fills[l.fill] {
				t.Errorf("%s: Slack's file fills #%06x, Clack's art doesn't", a.name, l.fill)
			}
			delete(fills, l.fill)
		}
		for f := range fills {
			t.Errorf("%s: Clack's art fills #%06x, Slack's file doesn't", a.name, f)
		}
		for _, px := range []int{a.px, 2 * a.px} {
			for _, l := range layers {
				ref := make([]float64, px*px)
				for _, sh := range l.shapes {
					addCoverage(ref, px, sh.edges(float64(px)/float64(view)), sh.evenOdd)
				}
				got := artCoverage(t, def, px, l.fill)
				var lo, hi, sa, sb float64
				for i := range ref {
					a, b := min(max(ref[i], 0), 1), got[i]
					lo += min(a, b)
					hi += max(a, b)
					sa += a
					sb += b
				}
				iou, ratio := lo/hi, sb/sa
				t.Logf("%s %dpx #%06x: IoU %.3f, area %.3f of Slack's", a.name, px, l.fill, iou, ratio)
				if iou < artIoU || math.Abs(ratio-1) > artInk {
					t.Errorf("%s %dpx #%06x: IoU %.3f, area %.3f of Slack's (want IoU ≥ %.2f, area within %.0f%%)", a.name, px, l.fill, iou, ratio, artIoU, artInk*100)
				}
			}
			if shots != "" {
				writeArtSheet(t, filepath.Join(shots, fmt.Sprintf("art-vs-slack-%s-%d.png", a.name, px)), def, layers, view, px)
			}
		}
	}
}

// TestArtParts checks the illustrations without Slack's files: every part
// parses into a closed shape inside its view, and the logo keeps its four
// colours, two shapes each.
func TestArtParts(t *testing.T) {
	for _, a := range artRef {
		if !HasArt(a.name) {
			t.Errorf("artRef names %q, which isn't registered", a.name)
		}
	}
	for name, d := range artRegistry {
		for i, p := range d.parts {
			segs := parsePath(p.d)
			if len(segs) == 0 || segs[0].op != 'M' || segs[len(segs)-1].op != 'Z' {
				t.Errorf("%s part %d: bad path %q", name, i, p.d)
			}
			for _, s := range segs {
				for _, pt := range s.p {
					if pt.X < -1 || pt.X > d.view+1 || pt.Y < -1 || pt.Y > d.view+1 {
						t.Errorf("%s part %d: point %v outside its %g view", name, i, pt, d.view)
					}
				}
			}
		}
	}
	cols := map[uint32]int{}
	for _, p := range artRegistry["slack-logo-mark"].parts {
		cols[p.fill]++
	}
	if len(cols) != 4 || cols[0xe3066a] != 2 || cols[0x00b3ff] != 2 || cols[0x41b658] != 2 || cols[0xfcc003] != 2 {
		t.Errorf("logo colours %v", cols)
	}
}

// TestArtRefsExist checks every literal illustration name the code draws
// (icons.Art's second argument) is registered.
func TestArtRefsExist(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	n := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if b := d.Name(); path != root && (b == "testdata" || b == "node_modules" || strings.HasPrefix(b, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(nd ast.Node) bool {
			c, ok := nd.(*ast.CallExpr)
			if !ok || len(c.Args) < 2 {
				return true
			}
			sel, ok := c.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Art" {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "icons" {
				return true
			}
			if lit, ok := c.Args[1].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				v, _ := strconv.Unquote(lit.Value)
				n++
				if !HasArt(v) {
					t.Errorf("%s: no illustration %q", fset.Position(lit.Pos()), v)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Error("found no icons.Art literal: the scan missed the tree")
	}
}

type svgShape struct {
	d       string  // path data
	ang     float64 // rotate() degrees about (cx, cy)
	cx, cy  float64
	evenOdd bool
}

// edges flattens the shape scaled by k, its rotation applied.
func (s svgShape) edges(k float64) []edge {
	es := flatten(parsePath(s.d), k)
	if s.ang == 0 {
		return es
	}
	sin, cos := math.Sincos(s.ang * math.Pi / 180)
	cx, cy := s.cx*k, s.cy*k
	rot := func(x, y float64) (float64, float64) {
		x, y = x-cx, y-cy
		return cx + x*cos - y*sin, cy + x*sin + y*cos
	}
	for i := range es {
		es[i].x0, es[i].y0 = rot(es[i].x0, es[i].y0)
		es[i].x1, es[i].y1 = rot(es[i].x1, es[i].y1)
	}
	return es
}

type svgLayer struct {
	fill   uint32
	shapes []svgShape
}

var (
	svgShapeRe = regexp.MustCompile(`<(path|rect|circle)\b[^>]*>`)
	svgViewRe  = regexp.MustCompile(`viewBox="0 0 ([0-9.]+) [0-9.]+"`)
	svgRotRe   = regexp.MustCompile(`rotate\(([-0-9.]+)[ ,]+([-0-9.]+)[ ,]+([-0-9.]+)\)`)
)

// svgLayers reads an illustration's <path>, <rect> (rx, rotate()) and
// <circle> elements by fill colour, in order of first use, and its view.
func svgLayers(t *testing.T, svg string) ([]svgLayer, float32) {
	t.Helper()
	m := svgViewRe.FindStringSubmatch(svg)
	if m == nil {
		t.Fatal("no viewBox")
	}
	view, _ := strconv.ParseFloat(m[1], 32)
	gEven := svgGEvenRe.MatchString(svg)
	var out []svgLayer
	idx := map[uint32]int{}
	num := func(a map[string]string, k string) float64 {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(a[k], "px"), 64)
		return v
	}
	for _, tag := range svgShapeRe.FindAllStringSubmatch(svg, -1) {
		a := map[string]string{}
		for _, m := range svgAttrRe.FindAllStringSubmatch(tag[0], -1) {
			a[m[1]] = m[2]
		}
		fill, ok := strings.CutPrefix(a["fill"], "#")
		if !ok {
			continue
		}
		col, err := strconv.ParseUint(fill, 16, 32)
		if err != nil {
			t.Fatalf("fill %q", a["fill"])
		}
		sh := svgShape{evenOdd: a["fill-rule"] == "evenodd" || a["fill-rule"] == "" && gEven}
		switch tag[1] {
		case "path":
			sh.d = a["d"]
		case "circle":
			cx, cy, r := num(a, "cx"), num(a, "cy"), num(a, "r")
			sh.d = fmt.Sprintf("M%g %g A%g %g 0 1 1 %g %g A%g %g 0 1 1 %g %gZ", cx-r, cy, r, r, cx+r, cy, r, r, cx-r, cy)
		case "rect":
			x, y, w, h, r := num(a, "x"), num(a, "y"), num(a, "width"), num(a, "height"), num(a, "rx")
			if r == 0 {
				sh.d = fmt.Sprintf("M%g %g H%g V%g H%g Z", x, y, x+w, y+h, x)
			} else {
				sh.d = rrect(float32(x), float32(y), float32(w), float32(h), float32(r))
			}
			if m := svgRotRe.FindStringSubmatch(a["transform"]); m != nil {
				sh.ang, _ = strconv.ParseFloat(m[1], 64)
				sh.cx, _ = strconv.ParseFloat(m[2], 64)
				sh.cy, _ = strconv.ParseFloat(m[3], 64)
			}
		}
		c := uint32(col)
		i, ok := idx[c]
		if !ok {
			i = len(out)
			idx[c] = i
			out = append(out, svgLayer{fill: c})
		}
		out[i].shapes = append(out[i].shapes, sh)
	}
	return out, float32(view)
}

// artCoverage draws def's parts filled with fill in black on white
// through Gio and reads their coverage back (Gio blends in linear light:
// a pixel's linear value is 1-coverage).
func artCoverage(t *testing.T, def *artDef, px int, fill uint32) []float64 {
	t.Helper()
	var ops op.Ops
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	paint.FillShape(&ops, white, clip.Rect{Max: image.Pt(px, px)}.Op())
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(px, px))}
	def.draw(gtx, px, func(f uint32) (color.NRGBA, bool) {
		return color.NRGBA{A: 0xff}, f == fill
	})
	img := pixeltest.Render(t, px, px, &ops)
	out := make([]float64, px*px)
	for i := range out {
		v := float64(img.Pix[4*i+1]) / 255
		lin := v / 12.92
		if v > 0.04045 {
			lin = math.Pow((v+0.055)/1.055, 2.4)
		}
		out[i] = 1 - lin
	}
	return out
}

// writeArtSheet saves Slack's file composited in sRGB (Chrome's) beside
// Clack's art through Gio, on white and on #1a1d21.
func writeArtSheet(t *testing.T, path string, def *artDef, layers []svgLayer, view float32, px int) {
	t.Helper()
	const gap = 8
	bgs := []color.NRGBA{{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, {R: 0x1a, G: 0x1d, B: 0x21, A: 0xff}}
	sheet := image.NewRGBA(image.Rect(0, 0, gap+2*(px+gap), gap+len(bgs)*(px+gap)))
	draw.Draw(sheet, sheet.Bounds(), image.NewUniform(color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff}), image.Point{}, draw.Src)
	for row, bg := range bgs {
		ref := image.NewRGBA(image.Rect(0, 0, px, px))
		draw.Draw(ref, ref.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
		for _, l := range layers {
			cov := make([]float64, px*px)
			for _, sh := range l.shapes {
				addCoverage(cov, px, sh.edges(float64(px)/float64(view)), sh.evenOdd)
			}
			c := hexRGB(l.fill)
			for i, a := range cov {
				a = min(max(a, 0), 1)
				mix := func(f, b uint8) uint8 { return uint8(math.Round(a*float64(f) + (1-a)*float64(b))) }
				p := ref.Pix[4*i : 4*i+3]
				p[0], p[1], p[2] = mix(c.R, p[0]), mix(c.G, p[1]), mix(c.B, p[2])
			}
		}
		var ops op.Ops
		paint.FillShape(&ops, bg, clip.Rect{Max: image.Pt(px, px)}.Op())
		gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(px, px))}
		def.draw(gtx, px, nil)
		got := pixeltest.Render(t, px, px, &ops)
		y := gap + row*(px+gap)
		draw.Draw(sheet, image.Rect(gap, y, gap+px, y+px), ref, image.Point{}, draw.Src)
		draw.Draw(sheet, image.Rect(2*gap+px, y, 2*gap+2*px, y+px), got, image.Point{}, draw.Src)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, sheet); err != nil {
		t.Fatal(err)
	}
}
