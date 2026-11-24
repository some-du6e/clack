package imgcache

import (
	"context"
	"errors"
	"image"
	"image/color"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/imgcache/internal/budget"
	"github.com/some-du6e/clack/internal/slackasset"
)

// logoSamples are the Slack logo's colours at (x, y) in its 256-unit viewBox,
// sampled in the middle of each of its eight shapes, and the transparent
// centre and corners between them.
var logoSamples = []struct {
	name string
	x, y float64
	c    color.NRGBA
}{
	{"pink dot", 26.9, 161.37, color.NRGBA{0xe3, 0x06, 0x6a, 0xff}},
	{"pink bar", 94.13, 201.7, color.NRGBA{0xe3, 0x06, 0x6a, 0xff}},
	{"blue dot", 94.13, 26.9, color.NRGBA{0x00, 0xb3, 0xff, 0xff}},
	{"blue bar", 54, 94.13, color.NRGBA{0x00, 0xb3, 0xff, 0xff}},
	{"green dot", 228.6, 94.13, color.NRGBA{0x41, 0xb6, 0x58, 0xff}},
	{"green bar", 161.37, 54, color.NRGBA{0x41, 0xb6, 0x58, 0xff}},
	{"yellow dot", 161.37, 228.6, color.NRGBA{0xfc, 0xc0, 0x03, 0xff}},
	{"yellow bar", 201.7, 161.37, color.NRGBA{0xfc, 0xc0, 0x03, 0xff}},
	{"centre", 128, 128, color.NRGBA{}},
	{"top left", 8, 8, color.NRGBA{}},
	{"bottom right", 248, 248, color.NRGBA{}},
}

func nrgbaAt(img *image.RGBA, x, y int) color.NRGBA {
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

func near(a, b color.NRGBA, tol int) bool {
	d := func(x, y uint8) bool { v := int(x) - int(y); return v <= tol && v >= -tol }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B) && d(a.A, b.A)
}

// The Slack system user's avatar (getMemberProfileImage's n8qp,
// slackasset.SlackLogoMark) loads from the bytes Clack ships, never the
// network, and is drawn at the size asked for: Slack's four colours where
// its shapes are, transparent between them (BaseAvatar's isSystemUser
// c-avatar--transparent_bg: no tile behind it). Before, imgcache had no SVG
// decoder at all, so the logo was undecodable wherever it was drawn.
func TestBundledSlackLogoDraws(t *testing.T) {
	var mu sync.Mutex
	var fetched []string
	c := New(func(ctx context.Context, url string) ([]byte, error) {
		mu.Lock()
		fetched = append(fetched, url)
		mu.Unlock()
		return nil, errors.New("offline")
	}, "", nil)
	for _, px := range []int{16, 24, 32, 48, 72, 144, 512} {
		_, sz, _, _, imgs, err := c.loadImage(key{url: slackasset.SlackLogoMark, size: px})
		if err != nil {
			t.Fatalf("%d px: %v", px, err)
		}
		if sz != image.Pt(px, px) || len(imgs) != 1 {
			t.Fatalf("%d px: drawn %v (%d images), want %dx%d", px, sz, len(imgs), px, px)
		}
		if px < 32 {
			continue // the shapes are a pixel or two across
		}
		img := imgs[0]
		k := float64(px) / 256
		for _, s := range logoSamples {
			got := nrgbaAt(img, int(s.x*k), int(s.y*k))
			if !near(got, s.c, 2) {
				t.Errorf("%d px: %s at (%d,%d) = %v, want %v", px, s.name, int(s.x*k), int(s.y*k), got, s.c)
			}
		}
	}
	// a 2x avatar slot through Get, as the UI asks
	inval := make(chan struct{}, 4)
	c.SetInvalidate(func() { inval <- struct{}{} })
	if _, ok := c.Get(slackasset.SlackLogoMark, 72); !ok {
		waitInval(t, inval, "the logo", 5*time.Second)
	}
	op, ok := c.Get(slackasset.SlackLogoMark, 72)
	if !ok || op.Size() != image.Pt(72, 72) {
		t.Fatalf("Get: ok %v size %v, want a 72x72 picture", ok, op.Size())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(fetched) != 0 {
		t.Fatalf("the bundled logo was fetched: %q", fetched)
	}
}

// Only the bytes Clack ships are rasterized. The same SVG fetched from
// anywhere, Slack's own CDN included (USLACK's profile image_* are
// a.slack-edge.com/…/slack_system_user/slack_logo_mark.svg), stays
// undecodable, as every SVG was before; so does a fetch that answers for
// the bundled URL itself if it ever reached the network.
func TestNetworkSVGStaysUndecodable(t *testing.T) {
	logo, _ := slackasset.Get(slackasset.SlackLogoMark)
	c := New(func(ctx context.Context, url string) ([]byte, error) { return logo, nil }, "", nil)
	for _, u := range []string{
		"https://a.slack-edge.com/9cc0056/img/avatars/slack_system_user/slack_logo_mark.svg",
		"https://a.slack-edge.com/bv1-13-br/slack_logo_mark-a79a4bc.svg?x=1",
		"https://example.com/slack_logo_mark.svg",
	} {
		_, _, _, _, _, err := c.loadImage(key{url: u, size: 72})
		if !errors.Is(err, image.ErrFormat) {
			t.Errorf("%s: err %v, want undecodable (image.ErrFormat)", u, err)
		}
		if !permanent(err) {
			t.Errorf("%s: %v is retried", u, err)
		}
	}
	// the bundled URL with other bytes (decodeLoad's own check)
	_, _, _, _, _, err := c.decodeLoad(key{url: slackasset.SlackLogoMark, size: 72}, []byte(strings.Replace(string(logo), "#E3066A", "#000000", 1)), func() {})
	if !errors.Is(err, image.ErrFormat) {
		t.Fatalf("other bytes under the bundled URL: err %v, want undecodable", err)
	}
}

// rasterSVG reads only the subset it draws exactly and refuses the rest.
func TestRasterSVGRefusesWhatItCannotDraw(t *testing.T) {
	const head = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">`
	ok := head + `<g fill="#123"><path d="M0 0h16v16H0z"/></g></svg>`
	b := NewBudget()
	defer b.Release()
	img, err := rasterSVG([]byte(ok), 0, b)
	if err != nil {
		t.Fatalf("plain square: %v", err)
	}
	if got := nrgbaAt(img, 8, 8); !near(got, color.NRGBA{0x11, 0x22, 0x33, 0xff}, 1) {
		t.Fatalf("inherited #123 fill: %v", got)
	}
	for name, s := range map[string]string{
		"stroke":       head + `<path d="M0 0h16" stroke="#000"/></svg>`,
		"transform":    head + `<path d="M0 0h16v16z" transform="scale(2)"/></svg>`,
		"evenodd":      head + `<path d="M0 0h16v16z" fill-rule="evenodd"/></svg>`,
		"url fill":     head + `<path d="M0 0h16v16z" fill="url(#g)"/></svg>`,
		"currentcolor": head + `<path d="M0 0h16v16z" fill="currentColor"/></svg>`,
		"rect":         head + `<rect width="16" height="16"/></svg>`,
		"clip rx":      head + `<defs><clipPath id="c"><rect width="8" height="8" rx="2"/></clipPath></defs></svg>`,
		"clip g":       head + `<defs><clipPath id="c"><g/></clipPath></defs></svg>`,
		"clip units":   head + `<defs><clipPath id="c" clipPathUnits="objectBoundingBox"><rect width="1" height="1"/></clipPath></defs></svg>`,
		"no clip":      head + `<path d="M0 0h16v16z" clip-path="url(#nope)"/></svg>`,
		"clip ref":     head + `<path d="M0 0h16v16z" clip-path="inset(2px)"/></svg>`,
		"path in defs": head + `<defs><path d="M0 0h16v16z"/></defs></svg>`,
		"clip twice":   head + `<defs><clipPath id="c"><rect width="1" height="1"/></clipPath><clipPath id="c"><rect width="1" height="1"/></clipPath></defs></svg>`,
		"image":        head + `<image href="https://example.com/x.png"/></svg>`,
		"use":          head + `<use href="#a"/></svg>`,
		"style":        head + `<style>path{fill:red}</style></svg>`,
		"text":         head + `<text>hi</text></svg>`,
		"doctype":      `<!DOCTYPE svg [<!ENTITY a "aaaa">]>` + head + `</svg>`,
		"no viewBox":   `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16"></svg>`,
		"not svg":      `<html></html>`,
		"bad path":     head + `<path d="L0 0"/></svg>`,
		"huge":         `<svg xmlns="http://www.w3.org/2000/svg" width="100000" height="16" viewBox="0 0 16 16"></svg>`,
		"xlink":        head + `<path d="M0 0h1v1z" xmlns:xlink="http://www.w3.org/1999/xlink" xlink:href="#x"/></svg>`,
		"foreign elem": head + `<x:path xmlns:x="urn:x" d="M0 0h1v1z"/></svg>`,
	} {
		if _, err := rasterSVG([]byte(s), 16, b); err == nil {
			t.Errorf("%s: drawn, want refused", name)
		}
	}
}

// rasterSVG charges what it allocates: the RGBA picture (4 bytes a pixel)
// and x/image/vector's buffers, one uint32 a pixel up to 512 px a side, and
// above that a float32 accumulator plus a uint32 mask (12 bytes in all).
func TestRasterSVGChargesItsBuffers(t *testing.T) {
	const sq = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16"><path d="M0 0h16v16H0z"/></svg>`
	for _, c := range []struct {
		size  int
		perPx int64
	}{{16, 8}, {512, 8}, {513, 12}, {600, 12}, {svgMaxSide, 12}} {
		b := budget.NewLocal(1 << 30)
		img, err := rasterSVG([]byte(sq), c.size, b)
		if err != nil {
			t.Fatalf("%d px: %v", c.size, err)
		}
		if img.Bounds().Dx() != c.size {
			t.Fatalf("%d px: drew %v", c.size, img.Bounds())
		}
		if got, want := b.Used(), c.perPx*int64(c.size)*int64(c.size); got != want {
			t.Errorf("%d px: charged %d bytes (%d a pixel), want %d (%d a pixel)", c.size, got, got/int64(c.size*c.size), want, c.perPx)
		}
		b.Release()
	}
}

// parseSVG and rasterSVG bound the work a document can ask for: bytes,
// nesting, paths, commands across paths, and coordinates in user units
// and on the canvas.
func TestRasterSVGBoundsItsInput(t *testing.T) {
	const head = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">`
	square := `<path d="M0 0h16v16H0z"/>`
	b := NewBudget()
	defer b.Release()
	// up to each bound it draws
	if _, err := rasterSVG([]byte(head+strings.Repeat(square, svgMaxShapes)+`</svg>`), 16, b); err != nil {
		t.Fatalf("%d paths: %v", svgMaxShapes, err)
	}
	// svg, g×14, path: 16 levels
	nest := strings.Repeat(`<g>`, svgMaxDepth-2) + square + strings.Repeat(`</g>`, svgMaxDepth-2)
	if _, err := rasterSVG([]byte(head+nest+`</svg>`), 16, b); err != nil {
		t.Fatalf("%d levels: %v", svgMaxDepth, err)
	}
	big := `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 100000 100000"><path d="M0 0L100000 100000H0z"/></svg>`
	if _, err := rasterSVG([]byte(big), 16, b); err != nil {
		t.Fatalf("coordinates at the bound: %v", err)
	}
	// an arc whose radii overflow lands nowhere near its endpoints
	if _, err := parsePathData("M0 0A1e300 1e300 0 0 1 16 16z"); !errors.Is(err, errSVG) {
		t.Errorf("overflowing arc: err %v, want refused", err)
	}
	// half the commands in each of two paths: within one path's cap, over
	// the document's
	long := `<path d="M0 0` + strings.Repeat(` h1`, svgMaxSegs/2) + `"/>`
	for name, s := range map[string]string{
		"bytes":        head + `<!--` + strings.Repeat("x", svgMaxBytes) + `-->` + square + `</svg>`,
		"paths":        head + strings.Repeat(square, svgMaxShapes+1) + `</svg>`,
		"depth":        head + `<g>` + nest + `</g></svg>`,
		"commands":     head + long + long + `</svg>`,
		"coordinate":   head + `<path d="M0 -200000L0 16H8z"/></svg>`,
		"relative sum": head + `<path d="M0 0` + strings.Repeat(` l60000 0`, 2) + `z"/></svg>`,
		"viewBox":      `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 1e6 16">` + square + `</svg>`,
		"tiny viewBox": `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 0.0001 0.0001"><path d="M0 -1L0 1H1z"/></svg>`,
	} {
		if _, err := rasterSVG([]byte(s), 16, b); !errors.Is(err, errSVG) {
			t.Errorf("%s: err %v, want refused", name, err)
		}
	}
}

// Path data's grammar: implicit lineto after M, packed numbers and arc
// flags, relative commands, H/V, S/T reflections and Z.
func TestParsePathData(t *testing.T) {
	segs, err := parsePathData("M53.79 161.37a26.9 26.9 0 1 1-53.79 0 26.9 26.9 0 0 1 53.79 0Z")
	if err != nil {
		t.Fatal(err)
	}
	end := func(s svgSeg) [2]float64 {
		if s.op == 'C' {
			return s.p2
		}
		return s.p0
	}
	if segs[0].op != 'M' || segs[len(segs)-1].op != 'Z' {
		t.Fatalf("segments %v", segs)
	}
	// two half circles of r 26.9 about (26.9, 161.37): back at the start
	if e := end(segs[len(segs)-2]); e != [2]float64{53.79, 161.37} {
		t.Fatalf("arc ends at %v", e)
	}
	// every cubic ends on the circle (its centre sits 0.52 below the
	// chord: the chord, 53.79, is a little short of the diameter)
	for _, s := range segs {
		if s.op != 'C' {
			continue
		}
		p := s.p2
		r := (p[0]-26.895)*(p[0]-26.895) + (p[1]-161.37)*(p[1]-161.37)
		if r < 26*26 || r > 28*28 {
			t.Fatalf("arc point %v is %v² from the centre", p, r)
		}
	}
	if _, err := parsePathData("M0,0L1-1h.5.5v2c1 1 2 2 3 3s4 4 5 5q1 1 2 2t3 3a1 1 0 0110 10zm1 1"); err != nil {
		t.Fatalf("packed path: %v", err)
	}
	for _, bad := range []string{"", "0 0", "M0", "M0 0 A1 1 0 2 0 1 1", "M0 0z1 1", "M1e999 0"} {
		if _, err := parsePathData(bad); err == nil && bad != "" {
			t.Errorf("%q parsed", bad)
		}
	}
}

// A fill can be a CSS color keyword, in any case ("Black" is black);
// transparent draws nothing.
func TestRasterSVGNamedFills(t *testing.T) {
	const head = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">`
	b := NewBudget()
	defer b.Release()
	for fill, want := range map[string]color.NRGBA{
		"red": {0xff, 0, 0, 0xff}, "Black": {0, 0, 0, 0xff}, "rebeccapurple": {0x66, 0x33, 0x99, 0xff}, "transparent": {},
	} {
		img, err := rasterSVG([]byte(head+`<path d="M0 0h16v16H0z" fill="`+fill+`"/></svg>`), 0, b)
		if err != nil {
			t.Fatalf("%s: %v", fill, err)
		}
		if got := nrgbaAt(img, 8, 8); !near(got, want, 1) {
			t.Errorf("fill %s: %v, want %v", fill, got, want)
		}
	}
}

// clip-path="url(#id)" draws a <g>'s or <path>'s shapes only inside the
// clipPath's rects and paths, united; a clipPath can come after its use
// (in a closing <defs>); a group's clip and its path's own both apply.
func TestRasterSVGClipPath(t *testing.T) {
	const head = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">`
	b := NewBudget()
	defer b.Release()
	draw := func(name, s string) *image.RGBA {
		t.Helper()
		img, err := rasterSVG([]byte(s), 0, b)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return img
	}
	black := color.NRGBA{A: 0xff}
	left := draw("left half", head+`<g clip-path="url(#l)"><path d="M0 0h16v16H0z" fill="black"/></g>`+
		`<defs><clipPath id="l"><rect width="8" height="16" fill="white"/></clipPath></defs></svg>`)
	if got := nrgbaAt(left, 4, 8); !near(got, black, 1) {
		t.Errorf("inside the clip: %v, want black", got)
	}
	if got := nrgbaAt(left, 12, 8); got.A != 0 {
		t.Errorf("outside the clip: %v, want nothing", got)
	}
	union := draw("two rects", head+`<defs><clipPath id="u"><rect width="4" height="16"/><rect x="12" width="4" height="16"/></clipPath></defs>`+
		`<path d="M0 0h16v16H0z" clip-path="url(#u)"/></svg>`)
	for _, c := range []struct {
		x    int
		want bool
	}{{2, true}, {8, false}, {14, true}} {
		if got := nrgbaAt(union, c.x, 8).A != 0; got != c.want {
			t.Errorf("two rects at x=%d: drawn %v, want %v", c.x, got, c.want)
		}
	}
	both := draw("group and path clips", head+`<defs><clipPath id="l"><rect width="8" height="16"/></clipPath>`+
		`<clipPath id="t"><path d="M0 0h16v8H0z"/></clipPath></defs>`+
		`<g clip-path="url(#l)"><path d="M0 0h16v16H0z" clip-path="url(#t)"/></g></svg>`)
	for _, c := range []struct {
		x, y int
		want bool
	}{{4, 4, true}, {12, 4, false}, {4, 12, false}} {
		if got := nrgbaAt(both, c.x, c.y).A != 0; got != c.want {
			t.Errorf("both clips at (%d, %d): drawn %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

// The file_input dialog's sad cloud (slackasset.EmptyStateLowConnection,
// n9p0's RUFH) draws from the bytes Clack ships. Its fills are named
// ("black") and its clipPath covers the whole canvas, so it draws exactly
// as the same SVG without the clip does, with its purples and its
// near-white face.
func TestBundledEmptyStateCloudDraws(t *testing.T) {
	data, ok := slackasset.Get(slackasset.EmptyStateLowConnection)
	if !ok || !slackasset.IsSVG(slackasset.EmptyStateLowConnection) {
		t.Fatal("the cloud isn't a bundled SVG")
	}
	b := NewBudget()
	defer b.Release()
	img, err := rasterSVG(data, 224, b)
	if err != nil {
		t.Fatalf("the cloud: %v", err)
	}
	const clip = ` clip-path="url(#clip0_11458_14667)"`
	if !strings.Contains(string(data), clip) {
		t.Fatal("the cloud's group has no clip-path")
	}
	plain, err := rasterSVG([]byte(strings.Replace(string(data), clip, "", 1)), 224, b)
	if err != nil {
		t.Fatalf("the cloud without its clip: %v", err)
	}
	if img.Bounds() != plain.Bounds() || string(img.Pix) != string(plain.Pix) {
		t.Error("the whole-canvas clip changed the picture")
	}
	purple, face := false, false
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			c := nrgbaAt(img, x, y)
			purple = purple || near(c, color.NRGBA{0x83, 0x38, 0x8a, 0xff}, 2)
			face = face || near(c, color.NRGBA{0xf7, 0xf4, 0xf7, 0xff}, 2)
		}
	}
	if !purple || !face {
		t.Errorf("purple %v, near-white face %v: want both drawn", purple, face)
	}
}
