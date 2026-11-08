package icons

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
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

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

// TestIconsMatchSlack draws Clack's icons through Gio's GPU renderer and
// compares them with Slack's own SVG glyphs rasterized the way Chrome does
// (exact area coverage, blended in sRGB), at the composer's 18dp in Slack's
// dark (#b9babd on #222529) and light (#454447 on #f8f8f8) toolbar inks,
// at 1x and 2x. Ink is Σ(pixel-bg)/(ink-bg) on the green channel, so a
// stroke that is too heavy or too thin shows up whatever its shape; shape
// is the L1 distance between the two normalized ink maps.
//
// Slack's glyphs aren't in the repo: CI downloads the icon set the public
// web client ships (the Iconify JSON slack-datamine-plus extracts, see the
// icon-shots workflow) and points CLACK_SLACK_ICONS at it. ICON_SHOTS_DIR
// gets side-by-side strips (Slack above, Clack below).
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
	// The formatting bar (redrawn on Slack's centerlines): held to Slack's
	// ink and shape. The others are drawn from their own sketches, so only
	// their weight is reported.
	strict := [][2]string{
		{"bold", "bold"}, {"italic", "italic"}, {"underline", "underline"}, {"strike", "strikethrough"},
		{"paragraph", "paragraph"}, {"link", "link"}, {"list-ordered", "numbered-list"},
		{"list-bullet", "bulleted-list"}, {"quote", "quote"}, {"code", "code"},
		{"code-block", "code-block"}, {"divider", "divider"},
	}
	report := [][2]string{
		{"emoji", "emoji"}, {"mention", "mentions"}, {"plus", "plus"}, {"search", "search"},
		{"close", "close"}, {"edit", "edit"}, {"threads", "threads"}, {"bell", "notifications"},
		{"video", "video"}, {"mic", "microphone"}, {"slash", "slash-box"}, {"more", "ellipsis-horizontal-filled"},
	}
	inks := []struct {
		name   string
		fg, bg color.NRGBA
	}{
		{"dark", nrgb(0xb9babd), nrgb(0x222529)},
		{"light", nrgb(0x454447), nrgb(0xf8f8f8)},
	}
	const size = 18
	shots := os.Getenv("ICON_SHOTS_DIR")
	for _, ink := range inks {
		for _, scale := range []int{1, 2} {
			px := size * scale
			pairs := append(append([][2]string{}, strict...), report...)
			var row [2][]*image.RGBA
			var lines []string
			for i, pr := range pairs {
				body, ok := set.Icons[pr[1]]
				if !ok {
					t.Errorf("Slack's set has no %q", pr[1])
					continue
				}
				ref := slackRaster(t, body.Body, px, ink.fg, ink.bg)
				got := clackRaster(t, pr[0], px, scale, ink.fg, ink.bg)
				ri, gi := inkOf(ref, ink.fg, ink.bg), inkOf(got, ink.fg, ink.bg)
				ratio := gi / ri
				shape := shapeDiff(ref, got, ink.fg, ink.bg)
				lines = append(lines, fmt.Sprintf("%-13s ink %.3f shape %.2f", pr[0], ratio, shape))
				if i < len(strict) {
					if math.Abs(ratio-1) > 0.08 {
						t.Errorf("%s %dx %s: ink %.3f of Slack's %s (want within 8%%)", ink.name, scale, pr[0], ratio, pr[1])
					}
					if shape > 0.35 {
						t.Errorf("%s %dx %s: shape differs from Slack's %s by %.2f (want ≤ 0.35)", ink.name, scale, pr[0], pr[1], shape)
					}
				}
				row[0], row[1] = append(row[0], ref), append(row[1], got)
			}
			t.Logf("%s %dx (Clack/Slack):\n%s", ink.name, scale, strings.Join(lines, "\n"))
			if shots != "" {
				writeStrip(t, filepath.Join(shots, fmt.Sprintf("icons-vs-slack-%s-%dx.png", ink.name, scale)), row, px, ink.bg)
			}
		}
	}
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

// writeStrip saves rows of px×px tiles with 8px gaps.
func writeStrip(t *testing.T, path string, rows [2][]*image.RGBA, px int, bg color.NRGBA) {
	t.Helper()
	const gap = 8
	n := len(rows[0])
	img := image.NewRGBA(image.Rect(0, 0, gap+n*(px+gap), gap+2*(px+gap)))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = bg.R, bg.G, bg.B, 0xff
	}
	for r := range rows {
		for i, tile := range rows[r] {
			o := image.Pt(gap+i*(px+gap), gap+r*(px+gap))
			for y := 0; y < px; y++ {
				copy(img.Pix[img.PixOffset(o.X, o.Y+y):], tile.Pix[tile.PixOffset(0, y):tile.PixOffset(px, y)])
			}
		}
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
