package icons

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

// TestIconsMatchSlack draws Clack's icons through Gio's GPU renderer and
// compares them with Slack's own SVG glyphs rasterized the way Chrome does
// (exact area coverage, blended in sRGB), at the composer's 18dp in Slack's
// dark (#b9babd on #222529) and light (#454447 on #f8f8f8) toolbar inks,
// at 1x and 2x. Ink is Σ(pixel-bg)/(ink-bg) on the green channel, so a
// stroke that is too heavy or too thin, or a glyph drawn smaller, shows up
// whatever its shape; shape is the L1 distance between the two normalized
// ink maps.
//
// Every icon in slackRef is held to its Slack glyph: ink within inkTol (6%
// at 2x, 10% at 1x) and shape within shapeTol in both inks at both scales. Every icon name the
// code uses (TestIconRefsExist's scan, the -filled variants the rail and
// tabs draw when selected, Block Kit's icon_button names) must be in
// slackRef or clackOwn.
//
// Slack's glyphs aren't in the repo: CI downloads the icon set the public
// web client ships (the Iconify JSON slack-datamine-plus extracts, see the
// icon-shots workflow) and points CLACK_SLACK_ICONS at it. ICON_SHOTS_DIR
// gets a contact sheet per ink and scale (each cell: Slack left, Clack
// right, the name and Clack's ink ratio below, red when out of tolerance)
// and icons-vs-slack.tsv with every measurement.
func TestIconsMatchSlack(t *testing.T) {
	path := os.Getenv("CLACK_SLACK_ICONS")
	if path == "" {
		t.Skip("CLACK_SLACK_ICONS (Slack's icons.json) not set; the icon-shots workflow sets it")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var set struct {
		Icons map[string]struct{ Body string } `json:"icons"`
	}
	if err := json.Unmarshal(raw, &set); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(slackRef))
	for n := range slackRef {
		names = append(names, n)
	}
	sort.Strings(names)
	inks := []struct {
		name   string
		fg, bg color.NRGBA
	}{
		{"dark", nrgb(0xb9babd), nrgb(0x222529)},
		{"light", nrgb(0x454447), nrgb(0xf8f8f8)},
	}
	const size = 18
	shots := os.Getenv("ICON_SHOTS_DIR")
	tsv := []string{"icon\tslack\tink\tscale\tratio\tshape"}
	worst := map[string]float64{}
	for _, ink := range inks {
		for _, scale := range []int{1, 2} {
			px := size * scale
			var cells []sheetCell
			var lines []string
			for _, n := range names {
				sn := slackRef[n]
				body, ok := set.Icons[sn]
				if !ok {
					t.Errorf("%s: Slack's set has no %q", n, sn)
					continue
				}
				if !Has(n) {
					t.Errorf("slackRef lists %q, which isn't registered", n)
					continue
				}
				ref := slackRaster(t, body.Body, px, ink.fg, ink.bg)
				got := clackRaster(t, n, px, scale, ink.fg, ink.bg)
				ratio := inkOf(got, ink.fg, ink.bg) / inkOf(ref, ink.fg, ink.bg)
				shape := shapeDiff(ref, got, ink.fg, ink.bg)
				tol := inkTol[scale-1]
				bad := math.Abs(ratio-1) > tol || shape > shapeTol
				if bad {
					t.Errorf("%s %dx %s: ink %.3f of Slack's %s, shape %.2f (want ink within %.0f%%, shape ≤ %.2f)", ink.name, scale, n, ratio, sn, shape, tol*100, shapeTol)
				}
				worst[n] = max(worst[n], math.Abs(ratio-1))
				lines = append(lines, fmt.Sprintf("%-20s %-28s ink %.3f shape %.2f", n, sn, ratio, shape))
				tsv = append(tsv, fmt.Sprintf("%s\t%s\t%s\t%d\t%.3f\t%.3f", n, sn, ink.name, scale, ratio, shape))
				cells = append(cells, sheetCell{n, ref, got, ratio, bad})
			}
			t.Logf("%s %dx (Clack/Slack):\n%s", ink.name, scale, strings.Join(lines, "\n"))
			if shots != "" {
				writeSheet(t, filepath.Join(shots, fmt.Sprintf("icons-vs-slack-%s-%dx.png", ink.name, scale)), cells, px, ink.fg, ink.bg)
			}
		}
	}
	if shots != "" {
		if err := os.WriteFile(filepath.Join(shots, "icons-vs-slack.tsv"), []byte(strings.Join(tsv, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var far []string
	for _, n := range names {
		if worst[n] > 0.05 {
			far = append(far, fmt.Sprintf("%s %.1f%%", n, worst[n]*100))
		}
	}
	t.Logf("%d icons against Slack's; ink off by more than 5%% anywhere: %s", len(names), strings.Join(far, ", "))
}

// The tolerances TestIconsMatchSlack holds every slackRef icon to. Ink
// is held to 6% at 2x, where it measures the drawing. At 1x (18px) the
// pixel phase of each line and Gio's linear-light blending against
// Chrome's sRGB move ink by up to ~9% whatever the shape: quote (drawn on
// Slack's centrelines since the emoji-tabs work) reads 0.938 in dark at
// 1x and 0.996 at 2x, bookmark 0.908 and 0.984, plus-filled 1.097 in
// light at 1x and 1.007 at 2x, all within 1% of Slack's geometry when both
// are rasterized Chrome's way. So 1x gets 10%.
var inkTol = [2]float64{0.10, 0.06} // 1x, 2x

const shapeTol = 0.35

// TestIconCoverage checks that every icon the code draws is held to a
// Slack glyph (slackRef) or is one of Clack's own (clackOwn), and that the
// two tables name registered icons. It needs no GPU or download.
func TestIconCoverage(t *testing.T) {
	for n, sn := range slackRef {
		if !Has(n) {
			t.Errorf("slackRef[%q] (Slack's %q): not registered", n, sn)
		}
		if _, ok := clackOwn[n]; ok {
			t.Errorf("%q is in both slackRef and clackOwn", n)
		}
	}
	for n := range clackOwn {
		if !Has(n) {
			t.Errorf("clackOwn[%q]: not registered", n)
		}
	}
	for _, n := range drawnIcons(t) {
		_, ok := slackRef[n]
		_, own := clackOwn[n]
		if !ok && !own {
			t.Errorf("icon %q is drawn but has no Slack glyph in slackRef (or a reason in clackOwn)", n)
		}
	}
}

// drawnIcons is every registered icon the code can draw: the literal names
// TestIconRefsExist finds (resolved through Slack's names), their -filled
// variants (the rail and the channel tabs draw name+"-filled" when
// selected); the scan includes the Slack names Block Kit's icon_button
// maps to.
func drawnIcons(t *testing.T) []string {
	refs, _ := iconRefs(t)
	seen := map[string]bool{}
	add := func(name string) {
		if n, ok := slackLookup(name); ok {
			seen[n] = true
			if Has(n + "-filled") {
				seen[n+"-filled"] = true
			}
		}
	}
	for _, r := range refs {
		add(r.name)
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func nrgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

// clackRaster draws the named icon at px through Gio's GPU renderer.
func clackRaster(t *testing.T, name string, px, scale int, fg, bg color.NRGBA) *image.RGBA {
	t.Helper()
	if !Has(name) {
		t.Fatalf("no Clack icon %q", name)
	}
	var ops op.Ops
	paint.FillShape(&ops, bg, clip.Rect{Max: image.Pt(px, px)}.Op())
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: float32(scale), PxPerSp: float32(scale)}, Constraints: layout.Exact(image.Pt(px, px))}
	Get(name)(gtx, unit.Dp(px/scale), fg)
	return pixeltest.Render(t, px, px, &ops)
}

var (
	svgPathRe  = regexp.MustCompile(`<path[^>]*>`)
	svgGEvenRe = regexp.MustCompile(`<g[^>]*fill-rule="evenodd"`)
	svgAttrRe  = regexp.MustCompile(`([a-z-]+)="([^"]*)"`)
)

// slackRaster fills the <path>s of an Iconify body (viewBox 0 0 20 20) at
// px with exact area coverage and blends them in sRGB, as Chrome does.
func slackRaster(t *testing.T, body string, px int, fg, bg color.NRGBA) *image.RGBA {
	t.Helper()
	cov := make([]float64, px*px)
	for _, tag := range svgPathRe.FindAllString(body, -1) {
		attrs := map[string]string{}
		for _, m := range svgAttrRe.FindAllStringSubmatch(tag, -1) {
			attrs[m[1]] = m[2]
		}
		if attrs["d"] == "" {
			continue
		}
		evenOdd := attrs["fill-rule"] == "evenodd" || attrs["fill-rule"] == "" && svgGEvenRe.MatchString(body)
		addCoverage(cov, px, flatten(parsePath(attrs["d"]), float64(px)/view), evenOdd)
	}
	img := image.NewRGBA(image.Rect(0, 0, px, px))
	for i, a := range cov {
		a = min(max(a, 0), 1)
		mix := func(f, b uint8) uint8 { return uint8(math.Round(a*float64(f) + (1-a)*float64(b))) }
		img.Pix[4*i], img.Pix[4*i+1], img.Pix[4*i+2], img.Pix[4*i+3] = mix(fg.R, bg.R), mix(fg.G, bg.G), mix(fg.B, bg.B), 0xff
	}
	return img
}

type edge struct{ x0, y0, x1, y1 float64 }

// flatten turns parsed segments into line edges (closing every contour).
func flatten(segs []seg, k float64) []edge {
	var out []edge
	var cur, start f32.Point
	open := false
	line := func(a, b f32.Point) {
		out = append(out, edge{float64(a.X) * k, float64(a.Y) * k, float64(b.X) * k, float64(b.Y) * k})
	}
	closeC := func() {
		if open && cur != start {
			line(cur, start)
		}
		cur, open = start, false
	}
	for _, s := range segs {
		switch s.op {
		case 'M':
			closeC()
			cur, start, open = s.p[0], s.p[0], true
		case 'L':
			line(cur, s.p[0])
			cur = s.p[0]
		case 'C':
			const n = 16
			p0, p1, p2, p3 := cur, s.p[0], s.p[1], s.p[2]
			prev := p0
			for i := 1; i <= n; i++ {
				u := float32(i) / n
				v := 1 - u
				q := p0.Mul(v * v * v).Add(p1.Mul(3 * v * v * u)).Add(p2.Mul(3 * v * u * u)).Add(p3.Mul(u * u * u))
				line(prev, q)
				prev = q
			}
			cur = p3
		case 'Z':
			closeC()
		}
	}
	closeC()
	return out
}

// addCoverage accumulates the area coverage of a filled contour set:
// 64 sub-scanlines per pixel row, exact horizontal span overlap.
func addCoverage(cov []float64, px int, edges []edge, evenOdd bool) {
	const sub = 64
	type xing struct {
		x float64
		w int
	}
	var xs []xing
	for row := 0; row < px*sub; row++ {
		y := (float64(row) + 0.5) / sub
		xs = xs[:0]
		for _, e := range edges {
			if (e.y0 <= y) == (e.y1 <= y) {
				continue
			}
			w := 1
			if e.y1 < e.y0 {
				w = -1
			}
			xs = append(xs, xing{e.x0 + (y-e.y0)*(e.x1-e.x0)/(e.y1-e.y0), w})
		}
		sort.Slice(xs, func(i, j int) bool { return xs[i].x < xs[j].x })
		wind := 0
		for i := 0; i+1 < len(xs); i++ {
			wind += xs[i].w
			in := wind != 0
			if evenOdd {
				in = wind%2 != 0
			}
			if !in {
				continue
			}
			a, b := max(xs[i].x, 0), min(xs[i+1].x, float64(px))
			for x := int(a); x < px && float64(x) < b; x++ {
				lo, hi := max(a, float64(x)), min(b, float64(x+1))
				if hi > lo {
					cov[(row/sub)*px+x] += (hi - lo) / sub
				}
			}
		}
	}
}

// inkOf is Σ(pixel-bg)/(fg-bg) on the green channel, in pixels.
func inkOf(img *image.RGBA, fg, bg color.NRGBA) float64 {
	d := float64(fg.G) - float64(bg.G)
	var s float64
	for i := 1; i < len(img.Pix); i += 4 {
		s += (float64(img.Pix[i]) - float64(bg.G)) / d
	}
	return s
}

// shapeDiff is Σ|a-b| / Σa over the two images' per-pixel ink.
func shapeDiff(ref, got *image.RGBA, fg, bg color.NRGBA) float64 {
	d := float64(fg.G) - float64(bg.G)
	var diff, tot float64
	for i := 1; i < len(ref.Pix); i += 4 {
		a := (float64(ref.Pix[i]) - float64(bg.G)) / d
		b := (float64(got.Pix[i]) - float64(bg.G)) / d
		diff += math.Abs(a - b)
		tot += a
	}
	return diff / tot
}

type sheetCell struct {
	name     string
	ref, got *image.RGBA
	ratio    float64
	bad      bool
}

// writeSheet saves a contact sheet: per icon, Slack's glyph and Clack's
// side by side with the name and ink ratio below (red when out of
// tolerance).
func writeSheet(t *testing.T, path string, cells []sheetCell, px int, fg, bg color.NRGBA) {
	t.Helper()
	const cols, gap, cw = 10, 8, 140
	face := basicfont.Face7x13
	ch := px + gap + 16
	rows := (len(cells) + cols - 1) / cols
	img := image.NewRGBA(image.Rect(0, 0, gap+cols*cw, gap+rows*ch))
	draw.Draw(img, img.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	for i, c := range cells {
		o := image.Pt(gap+(i%cols)*cw, gap+(i/cols)*ch)
		draw.Draw(img, image.Rectangle{Min: o, Max: o.Add(image.Pt(px, px))}, c.ref, image.Point{}, draw.Src)
		o2 := o.Add(image.Pt(px+4, 0))
		draw.Draw(img, image.Rectangle{Min: o2, Max: o2.Add(image.Pt(px, px))}, c.got, image.Point{}, draw.Src)
		lc := image.NewUniform(fg)
		if c.bad {
			lc = image.NewUniform(color.NRGBA{R: 0xe0, G: 0x1e, B: 0x5a, A: 0xff})
		}
		label := c.name
		if len(label) > 14 {
			label = label[:14]
		}
		d := font.Drawer{Dst: img, Src: lc, Face: face, Dot: fixed.P(o.X, o.Y+px+13)}
		d.DrawString(fmt.Sprintf("%s %.2f", label, c.ratio))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
