package inspect

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/clipimg"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// capture is a pick or a dragged region on its way to the clipboard:
// the report, built when the press was read (from the frame the pointer
// was over) or, for a region, at the release, waiting for the release's
// frame to be shot and, for a Shift+click or a region, the review
// (review.go) to finish.
type capture struct {
	rep    *Report
	rect   image.Rectangle // px, window
	ptr    image.Point     // px
	scale  float32
	region bool              // a dragged region: shot as it is, no margin, no outline
	review bool              // a Shift+click: reviewed before it's copied (a region always is)
	redact []image.Rectangle // the review's rects, px in the shot

	// once shot: the element and the pointer in the shot (px), and the
	// shot's image px per window px
	elem    image.Rectangle
	ptrShot image.Point
	k       float32
}

// Shoot renders the part of a frame's ops in region (window px), each px
// of it drawn as k: a ShotSize(region, k) image. The window's GPU can't
// read its frames back, so the ops go once more through a headless GPU
// context (a Metal device, D3D11, EGL), under a transform that scales
// the region up and moves it to the origin; paths and glyph outlines are
// vectors, so the region comes out as sharp as a window k times as dense
// (Gio patch 31, Ops.Whole). Tests replace it.
var Shoot = func(ops *op.Ops, region image.Rectangle, k float32) (*image.RGBA, error) {
	size := ShotSize(region, k)
	if size.X <= 0 || size.Y <= 0 {
		return nil, errors.New("empty region")
	}
	w, err := headless.NewWindow(size.X, size.Y)
	if err != nil {
		return nil, err
	}
	defer w.Release()
	var o op.Ops
	tr := op.Affine(f32.AffineId().Offset(f32.Pt(-float32(region.Min.X), -float32(region.Min.Y))).Scale(f32.Point{}, f32.Pt(k, k))).Push(&o)
	ops.Whole().Add(&o)
	tr.Pop()
	if err := w.Frame(&o); err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rectangle{Max: size})
	if err := w.Screenshot(img); err != nil {
		return nil, err
	}
	return img, nil
}

// ShotSize is the size of Shoot's image of region at k.
func ShotSize(region image.Rectangle, k float32) image.Point {
	return image.Pt(int(float32(region.Dx())*k+.5), int(float32(region.Dy())*k+.5))
}

// The pick's image shows the element, not the window: the element and
// CropMargin dp around it (blurred in Compose, so no neighbour's text
// can be read), drawn at ShotDensity px per dp so the pasted element
// stays sharp, at a lower density for an element so big its image would
// be wider or taller than MaxShotSide px, never below the window's own.
const (
	CropMargin  = 24
	ShotDensity = 4
	MaxShotSide = 3200
)

// cropFor is the part of a size px window the image of rect (px) shows,
// and how many image px each of its window px becomes.
func cropFor(rect image.Rectangle, size image.Point, scale float32) (image.Rectangle, float32) {
	m := int(math.Ceil(float64(CropMargin * scale)))
	crop := rect.Inset(-m).Intersect(image.Rectangle{Max: size})
	if crop.Empty() || rect.Intersect(crop).Empty() {
		return image.Rectangle{}, 1
	}
	return crop, density(crop, scale)
}

// regionCrop is cropFor for a dragged region: the region itself, inside
// the window, with no margin.
func regionCrop(rect image.Rectangle, size image.Point, scale float32) (image.Rectangle, float32) {
	crop := rect.Canon().Intersect(image.Rectangle{Max: size})
	if crop.Empty() {
		return image.Rectangle{}, 1
	}
	return crop, density(crop, scale)
}

// density is how many image px each window px of crop becomes:
// ShotDensity px per dp, less for a crop whose image would pass
// MaxShotSide, never below the window's own.
func density(crop image.Rectangle, scale float32) float32 {
	k := ShotDensity / scale
	if side := float32(max(crop.Dx(), crop.Dy())); side*k > MaxShotSide {
		k = MaxShotSide / side
	}
	return max(k, 1)
}

// inShot maps r (window px) into the shot of crop at k.
func inShot(r, crop image.Rectangle, k float32) image.Rectangle {
	m := func(v, o int) int { return int(float32(v-o)*k + .5) }
	return image.Rect(m(r.Min.X, crop.Min.X), m(r.Min.Y, crop.Min.Y), m(r.Max.X, crop.Min.X), m(r.Max.Y, crop.Min.Y))
}

// WriteImage puts the composed image on the clipboard (clipimg; tests
// replace it). Off the UI goroutine.
var WriteImage = func(img image.Image) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return clipimg.Write(ctx, img)
}

// newCapture builds the report for p (UI goroutine, before this frame
// lays anything out: p's frame still holds what the pointer was over).
func (t *Tool) newCapture(gtx layout.Context, p *pick) *capture {
	now := gtx.Now
	if now.IsZero() {
		now = time.Now()
	}
	scale := gtx.Metric.PxPerDp
	if scale <= 0 {
		scale = 1
	}
	red := NewRedactor()
	r := &Report{When: now, OS: goos()}
	ptr := image.Pt(int(t.ptr.X+.5), int(t.ptr.Y+.5))
	t.fill(r, red, p)
	t.host.Describe(r, red)
	r.Region = p.region
	r.Rect = dpRect(p.rect, scale)
	r.Pointer = image.Pt(int(float32(ptr.X)/scale+.5), int(float32(ptr.Y)/scale+.5))
	return &capture{rep: r, rect: p.rect, ptr: ptr, scale: scale}
}

// newRegionCapture builds the report for a dragged region (window px;
// ptr where the button came up): no component, the shell's areas it
// takes in, the window and the page.
func (t *Tool) newRegionCapture(gtx layout.Context, rect image.Rectangle, ptr image.Point) *capture {
	now := gtx.Now
	if now.IsZero() {
		now = time.Now()
	}
	scale := gtx.Metric.PxPerDp
	if scale <= 0 {
		scale = 1
	}
	red := NewRedactor()
	r := &Report{When: now, OS: goos(), Capture: "dragged region"}
	t.host.Describe(r, red)
	var in []string
	for _, rg := range t.host.Regions() {
		if rg.Name != "window" && rg.Rect.Overlaps(rect) {
			in = append(in, rg.Name)
		}
	}
	r.Region = strings.Join(in, ", ")
	r.Rect = dpRect(rect, scale)
	r.Pointer = image.Pt(int(float32(ptr.X)/scale+.5), int(float32(ptr.Y)/scale+.5))
	return &capture{rep: r, rect: rect, ptr: ptr, scale: scale, region: true}
}

// Limits on the chains a report lists.
const (
	maxWithin  = 14
	maxClasses = 6 // the nearest elements: "within" names where it is
	maxPropsOf = 8 // components whose props are read for ids, innermost first
	maxExps    = 30
)

// fill puts what slackc recorded about p into r.
func (t *Tool) fill(r *Report, red *Redactor, p *pick) {
	f := p.frame
	if p.node < 0 {
		return // the shell's area: no component
	}
	node := p.node
	if n := &f.Nodes[node]; n.Kind == slackc.InspectTree {
		for k, b := range f.Boxes(node, p.local) {
			if k == 0 {
				r.Style = boxStyle(b)
			}
			if len(r.Tree) < maxClasses {
				r.Tree = append(r.Tree, boxSelector(b))
			}
		}
		node = n.Parent // the component the plugin patches
	}
	seen := map[string]bool{}
	kinds := map[string]bool{}
	propsRead := 0
	for _, i := range f.Chain(node) {
		n := &f.Nodes[i]
		switch n.Kind {
		case slackc.InspectComponent:
			if r.Component == "" {
				r.Component, r.Patched = n.Name, n.Patched
				if el := rootElement(f, i); el >= 0 && r.Element == "" {
					r.Element = selector(&f.Nodes[el], red, 0)
				}
			} else if len(r.Within) < maxWithin {
				r.Within = append(r.Within, n.Name)
			}
			if propsRead < maxPropsOf {
				propsRead++
				pr := n.Props()
				for _, s := range red.PropIDs(pr) {
					if !seen[s] {
						seen[s] = true
						r.IDs = append(r.IDs, s)
					}
				}
				for _, id := range PropChannels(pr) {
					if k := t.host.ConvKind(id); k != "" {
						kinds[red.ID(id)+" "+k] = true
					}
				}
			}
		case slackc.InspectElement:
			switch {
			case i == node:
				// a sized element picked itself (the chrome's: the header's
				// title, a tab): the component is the one around it
				r.Element = selector(n, red, 0)
			case (r.Component != "" || r.Element != "") && len(r.Classes) < maxClasses:
				r.Classes = append(r.Classes, selector(n, red, 0))
			}
		}
	}
	for k := range kinds {
		r.Kinds = append(r.Kinds, k)
	}
	sort.Strings(r.Kinds)
	r.Exps = experiments(f, node)
}

// experiments are the experiments read while node and what it contains
// drew, then those its enclosing components read, at most maxExps.
func experiments(f *slackc.InspectFrame, node int) []string {
	if node < 0 || node >= len(f.Nodes) {
		return nil // a plugin tree at the top: no component drew it
	}
	set := map[string]bool{}
	var own []string
	add := func(list *[]string, e string) {
		if !set[e] {
			set[e] = true
			*list = append(*list, e)
		}
	}
	for j := node; j < len(f.Nodes); j++ {
		if j > node && !descends(f, j, node) {
			break
		}
		for _, e := range f.Nodes[j].Exps {
			add(&own, e)
		}
	}
	sort.Strings(own)
	var around []string
	for _, i := range f.Chain(f.Nodes[node].Parent) {
		for _, e := range f.Nodes[i].Exps {
			add(&around, e)
		}
	}
	sort.Strings(around)
	for i := range around {
		around[i] += " (around it)"
	}
	out := append(own, around...)
	if len(out) > maxExps {
		out = append(out[:maxExps], fmt.Sprintf("+%d more", len(out)-maxExps))
	}
	return out
}

func boxStyle(b slackc.InspectBox) []string {
	var s []string
	if b.Color.A > 0 {
		s = append(s, "color "+hexA(b.Color))
	}
	if b.Background.A > 0 {
		s = append(s, "background "+hexA(b.Background))
	}
	if b.FontSize > 0 {
		f := fmt.Sprintf("font %gpx %d", b.FontSize, b.FontWeight)
		if b.FontFamily != "" {
			f += " " + b.FontFamily
		}
		s = append(s, f)
	}
	return s
}

func dpRect(r image.Rectangle, scale float32) image.Rectangle {
	d := func(v int) int { return int(float32(v)/scale + .5) }
	return image.Rect(d(r.Min.X), d(r.Min.Y), d(r.Max.X), d(r.Max.Y))
}

// AfterFrame shoots the frame just finished when a pick or a dragged
// region asked for it. A plain click's report goes on its way with the
// shot at once, the session over; a Shift+click's or a region's shot is
// shown for review (review.go) first. With no shot the report goes on
// its way as text at once. Call it on the UI
// goroutine once the frame's ops are complete (after the window's frame
// was submitted, before the ops are reset); size is the frame's size in
// px.
func (t *Tool) AfterFrame(ops *op.Ops, size image.Point) {
	c := t.capture
	if c == nil {
		return
	}
	t.capture = nil
	crop, k := cropFor(c.rect, size, c.scale)
	if c.region {
		crop, k = regionCrop(c.rect, size, c.scale)
	}
	var img *image.RGBA
	err := errors.New("the element is outside the window")
	if !crop.Empty() {
		img, err = Shoot(ops, crop, k)
	}
	if err != nil {
		log.Printf("inspect: no screenshot: %v", err)
		t.stop()
		t.deliver(c, nil, err)
		return
	}
	c.elem = inShot(c.rect, crop, k)
	if c.region {
		c.elem = img.Bounds() // all of it, however it rounded
	}
	// the middle of the pointer's px
	c.ptrShot = image.Pt(int((float32(c.ptr.X-crop.Min.X)+.5)*k), int((float32(c.ptr.Y-crop.Min.Y)+.5)*k))
	c.k = k
	if !c.region && !c.review {
		t.stop()
		t.deliver(c, img, nil)
		return
	}
	t.startReview(c, img)
}

// deliver composes, saves and copies the report off the UI goroutine,
// then says what it did in a toast.
func (t *Tool) deliver(c *capture, shot *image.RGBA, shotErr error) {
	dir := t.host.ConfigDir()
	h := t.host
	done := make(chan struct{})
	t.lastDone, t.lastRep = done, c.rep
	go func() {
		defer close(done)
		if shot != nil {
			c.rep.Colors = sampleColors(shot, c.elem, c.ptrShot, max(int(c.k), 1))
		}
		lines := c.rep.Lines()
		text := c.rep.Text()
		var img *image.RGBA
		var data []byte
		if shot != nil {
			var err error
			if img, err = Compose(shot, c.elem, c.scale*c.k, lines, c.redact...); err != nil {
				log.Printf("inspect: composing the report image: %v", err)
				img = nil
			} else {
				var b bytes.Buffer
				if err := png.Encode(&b, img); err == nil {
					data = b.Bytes()
				}
			}
		}
		where, serr := save(dir, c.rep.When, data, text)
		if serr != nil {
			log.Printf("inspect: saving the report: %v", serr)
		} else if where != "" {
			log.Printf("inspect: report saved as %s", where)
		}
		var cerr error = shotErr
		if img != nil {
			cerr = WriteImage(img)
		}
		h.Post(func() {
			switch {
			case img != nil && cerr == nil:
				h.Toast("Copied the inspect report: paste it as an image")
			default:
				h.CopyText(text)
				why := "no screenshot"
				if errors.Is(cerr, clipimg.ErrUnsupported) {
					why = "this system can't copy images"
				}
				h.Toast("Copied the inspect report as text (" + why + ")")
			}
		})
	}()
}

// save writes the report's image and text to dir/inspect-reports (only
// the owner can read them: the image shows the element). It
// returns the files' common stem.
func save(dir string, when time.Time, img []byte, text string) (string, error) {
	if dir == "" {
		return "", nil
	}
	d := filepath.Join(dir, "inspect-reports")
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	stem := filepath.Join(d, "inspect-"+when.Format("20060102-150405"))
	if img != nil {
		if err := os.WriteFile(stem+".png", img, 0o600); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(stem+".txt", []byte(text), 0o600); err != nil {
		return "", err
	}
	return stem, nil
}

// sampleColors reads the shot at the element: its background (the most
// common colour in rect), its ink (the most common one clearly unlike the
// background) and the pixel under the pointer. It counts every step'th
// pixel each way (the shot is denser than the window).
func sampleColors(img *image.RGBA, rect image.Rectangle, ptr image.Point, step int) []string {
	var out []string
	r := rect.Intersect(img.Bounds())
	step = max(step, 1)
	if !r.Empty() {
		counts := map[color.RGBA]int{}
		for y := r.Min.Y; y < r.Max.Y; y += step {
			for x := r.Min.X; x < r.Max.X; x += step {
				counts[img.RGBAAt(x, y)]++
			}
		}
		type cc struct {
			c color.RGBA
			n int
		}
		list := make([]cc, 0, len(counts))
		for c, n := range counts {
			list = append(list, cc{c, n})
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].n != list[j].n {
				return list[i].n > list[j].n
			}
			return rgbaKey(list[i].c) < rgbaKey(list[j].c)
		})
		bg := list[0].c
		out = append(out, "bg "+hex6(nrgba(bg)))
		for _, x := range list[1:] {
			if dist(x.c, bg) > 60 {
				out = append(out, "ink "+hex6(nrgba(x.c)))
				break
			}
		}
	}
	if ptr.In(img.Bounds()) {
		out = append(out, "under the pointer "+hex6(nrgba(img.RGBAAt(ptr.X, ptr.Y))))
	}
	return out
}

func rgbaKey(c color.RGBA) uint32 {
	return uint32(c.R)<<24 | uint32(c.G)<<16 | uint32(c.B)<<8 | uint32(c.A)
}

func dist(a, b color.RGBA) int {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) + d(a.G, b.G) + d(a.B, b.B)
}

// nrgba un-premultiplies (the shot is opaque, so this is a copy).
func nrgba(c color.RGBA) color.NRGBA {
	if c.A == 0xff || c.A == 0 {
		return color.NRGBA{R: c.R, G: c.G, B: c.B, A: c.A}
	}
	return color.NRGBAModel.Convert(c).(color.NRGBA)
}

// Wait blocks until the last report was saved and copied (tests).
func (t *Tool) Wait() {
	if t.lastDone != nil {
		<-t.lastDone
	}
}
