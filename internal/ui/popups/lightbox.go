package popups

import (
	"context"
	"errors"
	"image"
	"math"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Measured from app.slack.com (2026-11, media viewer): a panel inset 28px
// from the window edges, bg rgba(0,0,0,.7) over the modal backdrop
// rgba(29,28,29,.7). Header gradient 85 tall rgba(29,28,29,.6)→0 with the
// sender avatar 36 (radius 8) at (12,14), name 15/900 white and "1 hour ago
// in # channel – image.png" 13px white; close X 36x36 inset 8. Footer
// gradient 85 tall: Rotate at 12 from the left, then [− slider +] (slider
// knob #1d9bd1); on the right Download, Show thread, Open in new window,
// More actions, 36x36 white icon buttons 12 apart, 12 from the edge,
// centered 27px above the bottom. The image is centered and fitted into
// the area between header and footer (36px side margins), never upscaled.

// lbItem is one viewable image of the message.
type lbItem struct {
	img  richtext.Image
	file *richtext.File // set for shared files
}

// Lightbox is the full-window image viewer.
type Lightbox struct {
	base
	h     Host
	m     core.Message
	items []lbItem
	idx   int

	zoom float32 // 1..4
	rot  int     // quarter turns clockwise
	pan  f32.Point

	bgTag, imgTag, sliderTag int
	headBlock, footBlock     blocker
	dragging                 bool
	dragFrom, panFrom        f32.Point
	sliding                  bool
	focus                    bool
	busy                     bool // download in flight

	closeBtn, rotBtn, outBtn, inBtn, resetBtn widget.Clickable
	dlBtn, threadBtn, winBtn, moreBtn         widget.Clickable
	prevBtn, nextBtn                          widget.Clickable
	tips                                      [9]TipState
	moreRect                                  image.Rectangle
}

const (
	lbMinZoom = 1
	lbMaxZoom = 4
)

// NewLightbox opens img (one of m's images) full-window. Every image of the
// message can be browsed with the arrows.
func NewLightbox(h Host, img richtext.Image, m core.Message) *Lightbox {
	lb := &Lightbox{h: h, m: m, zoom: 1}
	lbCollect(lb.m.Doc.Blocks, &lb.items)
	lb.idx = -1
	for i, it := range lb.items {
		if lbSame(it, img) {
			lb.idx = i
			break
		}
	}
	if lb.idx < 0 {
		lb.items = append([]lbItem{{img: img}}, lb.items...)
		lb.idx = 0
	}
	return lb
}

// lbCollect appends every image in blocks (image blocks, image files with a
// thumb, attachment images), recursively, skipping duplicates.
func lbCollect(blocks []richtext.Block, out *[]lbItem) {
	add := func(it lbItem) {
		if it.img.URL == "" {
			return
		}
		for _, o := range *out {
			if o.img.URL == it.img.URL {
				return
			}
		}
		*out = append(*out, it)
	}
	for i := range blocks {
		b := &blocks[i]
		switch b.Kind {
		case richtext.ImageBlock:
			if b.Image != nil {
				add(lbItem{img: *b.Image})
			}
		case richtext.FileBlock:
			if f := b.File; f != nil && f.IsImage && f.Thumb != nil {
				im := *f.Thumb
				if im.Title == "" {
					im.Title = f.Title
				}
				if im.Title == "" {
					im.Title = f.Name
				}
				add(lbItem{img: im, file: f})
			}
		case richtext.AttachmentBlock:
			if a := b.Att; a != nil {
				if a.Image != nil {
					add(lbItem{img: *a.Image})
				}
				lbCollect(a.Text, out)
			}
		}
		if len(b.Children) > 0 {
			lbCollect(b.Children, out)
		}
	}
}

// lbSame reports whether img (as passed by the message view) is item it.
func lbSame(it lbItem, img richtext.Image) bool {
	if img.URL == "" {
		return false
	}
	if it.img.URL == img.URL {
		return true
	}
	if f := it.file; f != nil {
		if f.URL == img.URL || f.Download == img.URL || f.Permalink == img.URL {
			return true
		}
		if f.ID != "" && strings.Contains(img.URL, f.ID) {
			return true // another thumb size of the same file
		}
	}
	return false
}

// displayURL is what the viewer shows for it: the original for decodable
// image files, the thumb otherwise.
func (it lbItem) displayURL() string {
	if f := it.file; f != nil && f.URL != "" {
		switch strings.ToLower(f.Mime) {
		case "image/png", "image/jpeg", "image/jpg", "image/gif", "image/webp":
			return f.URL
		}
	}
	return it.img.URL
}

// title is the file/image name shown in the header.
func (it lbItem) title() string {
	switch {
	case it.img.Title != "":
		return it.img.Title
	case it.file != nil && it.file.Title != "":
		return it.file.Title
	case it.file != nil && it.file.Name != "":
		return it.file.Name
	case it.img.Alt != "":
		return it.img.Alt
	}
	return "image"
}

// linkURL is the URL opened in the browser / copied: the file permalink
// when there is one.
func (it lbItem) linkURL() string {
	if it.file != nil && it.file.Permalink != "" {
		return it.file.Permalink
	}
	if it.file != nil && it.file.URL != "" {
		return it.file.URL
	}
	return it.img.URL
}

// hosted reports whether it is a Slack file: a shared file, or a
// files.slack.com image (the Files browser, canvases). Anything else (an
// unfurl's or attachment's image_url, an image block) is what Slack's
// AttachmentImage opens as a mediaItem, not a file: MediaViewerActions
// then draws only its own buttons (Show thread, Open in new window), not
// the file's (Download, More actions), the header has no "– title"
// (MediaViewerModalHeader shows it for file ids only), and there is no
// zoom control (fileIsImage needs a mimetype).
func (it lbItem) hosted() bool { return it.file != nil || it.img.Proxy }

// windowURL is what "Open in new window" opens: the file's link, or for
// an image that isn't a file openInNewWindow's image.src, which is the
// image through Slack's image proxy (getImgProxyUrl with rotate:
// slack-imgs.com ?c=1&o1=ro; Slack-hosted URLs as they are).
func (it lbItem) windowURL() string {
	if it.hosted() {
		return it.linkURL()
	}
	if u := slack.ProxyImage(it.img.URL); u != "" {
		return u
	}
	return it.img.URL
}

func (lb *Lightbox) cur() lbItem { return lb.items[lb.idx] }

func (lb *Lightbox) go2(i int) {
	if i < 0 || i >= len(lb.items) || i == lb.idx {
		return
	}
	lb.idx = i
	lb.resetView()
}

func (lb *Lightbox) resetView() {
	lb.zoom, lb.rot, lb.pan = 1, 0, f32.Point{}
	lb.dragging, lb.sliding = false, false
}

func (lb *Lightbox) setZoom(z float32) {
	if z < lbMinZoom {
		z = lbMinZoom
	}
	if z > lbMaxZoom {
		z = lbMaxZoom
	}
	if lb.zoom > 0 {
		lb.pan = lb.pan.Mul(z / lb.zoom)
	}
	lb.zoom = z
}

func (lb *Lightbox) wheel(dy float32) {
	if !lb.cur().hosted() {
		return // no zoom control for images that aren't files
	}
	f := float32(math.Exp(float64(-dy) * 0.02))
	if f < 0.8 {
		f = 0.8
	}
	if f > 1.25 {
		f = 1.25
	}
	lb.setZoom(lb.zoom * f)
}

// Layout draws the viewer.
func (lb *Lightbox) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	if lb.done {
		return layout.Dimensions{}
	}
	stacked(gtx, lb)
	th := lb.h.Theme()
	cl := colorsOf(th)
	win := gtx.Constraints.Max
	top := isTop(lb)
	if !lb.focus || (top && !gtx.Focused(lb)) {
		gtx.Execute(key.FocusCmd{Tag: lb})
		lb.focus = true
	}
	lb.keys(gtx, top)
	if lb.done {
		return layout.Dimensions{}
	}
	lb.clicks(gtx)
	if lb.done {
		return layout.Dimensions{}
	}
	closeReq := lb.pointerEvents(gtx)
	if closeReq {
		lb.Dismiss()
		return layout.Dimensions{}
	}

	full := image.Rectangle{Max: win}
	fill(gtx, full, cl.Backdrop)
	m28 := dp(gtx, 28)
	panel := full.Inset(m28)
	if panel.Dx() < dp(gtx, 200) || panel.Dy() < dp(gtx, 200) {
		panel = full
	}
	// key focus target lowest, then the full-window input catcher.
	keyTarget(gtx, lb, panel)
	lbArea(gtx, full, &lb.bgTag, pointer.CursorDefault)

	rad := dp(gtx, 8)
	pc := clipRRect(gtx, panel, rad)
	// Slack's media viewer panel is rgba(0,0,0,.7) over an opaque white
	// (dark: #1a1d21) layer, so it reads as a solid gray that hides the app;
	// only the 28px band around it shows the dimmed window (top nav and
	// rail included, via the backdrop above).
	under := rgb(0xffffff)
	if th.Dark {
		under = rgb(0x1a1d21)
	}
	fill(gtx, panel, theme.Mix(under, rgb(0x000000), .7))

	headH, footH := dp(gtx, 85), dp(gtx, 85)
	side := dp(gtx, 36)
	area := image.Rect(panel.Min.X+side, panel.Min.Y+headH, panel.Max.X-side, panel.Max.Y-footH)
	if area.Dx() < 1 || area.Dy() < 1 {
		area = panel
	}
	lb.drawImage(gtx, th, panel, area)

	// gradients
	hr := image.Rect(panel.Min.X, panel.Min.Y, panel.Max.X, panel.Min.Y+headH)
	fr := image.Rect(panel.Min.X, panel.Max.Y-footH, panel.Max.X, panel.Max.Y)
	lbGradient(gtx, hr, true)
	lbGradient(gtx, fr, false)
	pc.Pop()
	lb.headBlock.Layout(gtx, hr)
	lb.footBlock.Layout(gtx, fr)

	lb.header(gtx, th, panel)
	lb.footer(gtx, th, panel)
	lb.arrows(gtx, panel)
	return layout.Dimensions{Size: win}
}

func (lb *Lightbox) keys(gtx layout.Context, top bool) {
	for {
		e, ok := gtx.Event(
			key.Filter{Focus: lb, Name: key.NameEscape},
			key.Filter{Focus: lb, Name: key.NameLeftArrow},
			key.Filter{Focus: lb, Name: key.NameRightArrow},
			key.Filter{Focus: lb, Name: "+", Optional: key.ModShift},
			key.Filter{Focus: lb, Name: "=", Optional: key.ModShift},
			key.Filter{Focus: lb, Name: "-", Optional: key.ModShift},
			key.Filter{Focus: lb, Name: "0"},
		)
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		switch ke.Name {
		case key.NameEscape:
			if top {
				lb.Dismiss()
				return
			}
		case key.NameLeftArrow:
			lb.go2(lb.idx - 1)
		case key.NameRightArrow:
			lb.go2(lb.idx + 1)
		case "+", "=":
			if lb.cur().hosted() {
				lb.setZoom(lb.zoom + 0.5)
			}
		case "-":
			if lb.cur().hosted() {
				lb.setZoom(lb.zoom - 0.5)
			}
		case "0":
			lb.resetView()
		}
	}
}

func (lb *Lightbox) clicks(gtx layout.Context) {
	h := lb.h
	if lb.closeBtn.Clicked(gtx) {
		lb.Dismiss()
		return
	}
	if lb.rotBtn.Clicked(gtx) {
		lb.rot = (lb.rot + 1) % 4
		lb.pan = f32.Point{}
	}
	if lb.outBtn.Clicked(gtx) {
		lb.setZoom(lb.zoom - 0.5)
	}
	if lb.inBtn.Clicked(gtx) {
		lb.setZoom(lb.zoom + 0.5)
	}
	if lb.resetBtn.Clicked(gtx) {
		lb.resetView()
	}
	if lb.prevBtn.Clicked(gtx) {
		lb.go2(lb.idx - 1)
	}
	if lb.nextBtn.Clicked(gtx) {
		lb.go2(lb.idx + 1)
	}
	if lb.dlBtn.Clicked(gtx) {
		lb.download()
	}
	if lb.threadBtn.Clicked(gtx) {
		if lb.m.Channel == "" || lb.m.TS == "" {
			h.Toast("This image isn't part of a conversation")
		} else {
			root := lb.m.ThreadTS
			if root == "" {
				root = lb.m.TS
			}
			lb.Dismiss()
			h.OpenThread(lb.m.Channel, root)
			return
		}
	}
	if lb.winBtn.Clicked(gtx) {
		lbOpen(h, lb.cur().windowURL())
	}
	if lb.moreBtn.Clicked(gtx) {
		it := lb.cur()
		link := it.linkURL()
		items := []MenuItem{
			{Label: "Copy image link", Icon: "link", DoCtx: func(gtx layout.Context) {
				copyText(gtx, link)
				h.Toast("Link copied")
			}},
			{Label: "Open in browser", Icon: "external", Do: func() { lbOpen(h, link) }},
		}
		h.ShowOverlay(NewMenuOpts(h, items, MenuOpts{Anchor: lb.moreRect, Side: Above, AlignRight: true, Width: 240}))
	}
}

// pointerEvents handles the background, image and slider areas (last
// frame's registrations). It returns true when the dark area was clicked.
func (lb *Lightbox) pointerEvents(gtx layout.Context) bool {
	big := pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20}
	closeReq := false
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &lb.bgTag, Kinds: pointer.Press | pointer.Scroll, ScrollY: big, ScrollX: big})
		if !ok {
			break
		}
		pe, ok := e.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Press:
			if pe.Buttons == pointer.ButtonPrimary || pe.Buttons == 0 {
				closeReq = true
			}
		case pointer.Scroll:
			lb.wheel(pe.Scroll.Y)
		}
	}
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &lb.imgTag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Scroll, ScrollY: big, ScrollX: big})
		if !ok {
			break
		}
		pe, ok := e.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Press:
			lb.dragging = true
			lb.dragFrom, lb.panFrom = pe.Position, lb.pan
		case pointer.Drag:
			if lb.dragging {
				lb.pan = lb.panFrom.Add(pe.Position.Sub(lb.dragFrom))
			}
		case pointer.Release, pointer.Cancel:
			lb.dragging = false
		case pointer.Scroll:
			lb.wheel(pe.Scroll.Y)
		}
	}
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &lb.sliderTag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		pe, ok := e.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Press:
			lb.sliding = true
			lb.slideTo(gtx, pe.Position.X)
		case pointer.Drag:
			if lb.sliding {
				lb.slideTo(gtx, pe.Position.X)
			}
		case pointer.Release, pointer.Cancel:
			lb.sliding = false
		}
	}
	return closeReq
}

// slider geometry (dp, relative to the slider hit box).
const (
	lbTrackX0 = 5
	lbTrackW  = 47
	lbSlideW  = 57
)

func (lb *Lightbox) slideTo(gtx layout.Context, x float32) {
	t := (x - float32(dp(gtx, lbTrackX0))) / float32(dp(gtx, lbTrackW))
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	lb.setZoom(lbMinZoom + t*(lbMaxZoom-lbMinZoom))
}

// lbArea registers tag over r with a cursor.
func lbArea(gtx layout.Context, r image.Rectangle, tag event.Tag, c pointer.Cursor) {
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	if c != pointer.CursorDefault {
		c.Add(gtx.Ops)
	}
	event.Op(gtx.Ops, tag)
}

// lbGradient paints Slack's header (top) or footer scrim gradient.
func lbGradient(gtx layout.Context, r image.Rectangle, top bool) {
	c1 := rgba(0x1d1c1d, .6)
	c0 := c1
	c0.A = 0
	a, b := f32.Pt(0, float32(r.Min.Y)), f32.Pt(0, float32(r.Max.Y))
	if !top {
		a, b = b, a
	}
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	paint.LinearGradientOp{Stop1: a, Color1: c1, Stop2: b, Color2: c0}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// drawImage fits the current image into area (contain, at most natural
// size at zoom 1), applies zoom/rotation/pan and registers the drag area.
func (lb *Lightbox) drawImage(gtx layout.Context, th *theme.Theme, panel, area image.Rectangle) {
	it := lb.cur()
	imgs := lb.h.Images()
	u := it.displayURL()
	img, ok := imgs.Get(u, 0)
	if !ok && u != it.img.URL {
		img, ok = imgs.Get(it.img.URL, 0) // the thumb while the original loads
	}
	if !ok {
		msg := "Loading…"
		if imgs.Failed(u, 0) && (u == it.img.URL || imgs.Failed(it.img.URL, 0)) {
			msg = "This image couldn't be loaded"
		}
		tw := textW(gtx, th, 15, theme.Regular, msg)
		at(gtx, image.Pt(area.Min.X+(area.Dx()-tw)/2, area.Min.Y+area.Dy()/2-dp(gtx, 11)), func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Regular, rgba(0xffffff, .8), msg)
		})
		return
	}
	is := img.Size()
	if is.X <= 0 || is.Y <= 0 {
		return
	}
	nw, nh := float32(is.X), float32(is.Y)
	bw, bh := nw, nh
	if lb.rot%2 == 1 {
		bw, bh = nh, nw
	}
	s0 := gtx.Metric.PxPerDp
	if v := float32(area.Dx()) / bw; v < s0 {
		s0 = v
	}
	if v := float32(area.Dy()) / bh; v < s0 {
		s0 = v
	}
	s := s0 * lb.zoom
	dw, dh := bw*s, bh*s
	// keep the image covering the area when it overflows, centered otherwise
	mx := lbMaxF(0, (dw-float32(area.Dx()))/2)
	my := lbMaxF(0, (dh-float32(area.Dy()))/2)
	lb.pan.X = lbClampF(lb.pan.X, -mx, mx)
	lb.pan.Y = lbClampF(lb.pan.Y, -my, my)
	c := f32.Pt(float32(area.Min.X+area.Max.X)/2, float32(area.Min.Y+area.Max.Y)/2).Add(lb.pan)

	aff := f32.Affine2D{}.
		Offset(f32.Pt(-nw/2, -nh/2)).
		Scale(f32.Point{}, f32.Pt(s, s)).
		Rotate(f32.Point{}, float32(lb.rot)*math.Pi/2).
		Offset(c)
	t := op.Affine(aff).Push(gtx.Ops)
	cs := clip.Rect{Max: is}.Push(gtx.Ops)
	img.Filter = paint.FilterLinear
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	cs.Pop()
	t.Pop()

	r := image.Rect(int(c.X-dw/2), int(c.Y-dh/2), int(c.X+dw/2+.5), int(c.Y+dh/2+.5)).Intersect(panel)
	cur := pointer.CursorDefault
	if mx > 0 || my > 0 {
		cur = pointer.CursorGrab
		if lb.dragging {
			cur = pointer.CursorGrabbing
		}
	} else {
		lb.dragging = false
	}
	if !r.Empty() {
		lbArea(gtx, r, &lb.imgTag, cur)
	}
}

func (lb *Lightbox) header(gtx layout.Context, th *theme.Theme, panel image.Rectangle) {
	white := rgb(0xffffff)
	m := lb.m
	name := m.Name
	av := m.Avatar
	if u, ok := lookupUser(lb.h, m.User); ok {
		if pfBlank(name) {
			name = pfName(u)
		}
		if av == "" {
			av = u.Avatar
		}
	}
	if pfBlank(name) {
		name = "Unknown"
	}
	p := panel.Min.Add(image.Pt(dp(gtx, 12), dp(gtx, 14)))
	at(gtx, p, func(gtx layout.Context) layout.Dimensions {
		return pfImage(gtx, lb.h, av, 36, 8)
	})
	tx := p.X + dp(gtx, 36) + dp(gtx, 8)
	tw := maxInt(1, panel.Max.X-dp(gtx, 60)-tx)
	at(gtx, image.Pt(tx, p.Y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(tw, dp(gtx, 20))}
		return para(gtx, th, 15, 20, theme.Black, white, name, 1)
	})
	at(gtx, image.Pt(tx, p.Y+dp(gtx, 20)), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(tw, dp(gtx, 18))}
		return para(gtx, th, 13, 18, theme.Regular, white, lb.subLine(gtx), 1)
	})
	cp := image.Pt(panel.Max.X-dp(gtx, 8)-dp(gtx, 36), panel.Min.Y+dp(gtx, 8))
	at(gtx, cp, func(gtx layout.Context) layout.Dimensions {
		d := iconButton(gtx, &lb.closeBtn, "close", 36, 20, white, rgba(0xffffff, .1), 8)
		Tooltip(gtx, th, &lb.tips[8], lb.closeBtn.Hovered(), d.Size, Below, "Close", "Esc")
		return d
	})
}

// subLine is "1 hour ago in # channel – image.png".
func (lb *Lightbox) subLine(gtx layout.Context) string {
	var b strings.Builder
	if t := tsTime(lb.m.TS); !t.IsZero() {
		b.WriteString(ago(t, gtx.Now))
	}
	if ch := lb.m.Channel; ch != "" && lb.h.Core() != nil {
		var where string
		if c, ok := lb.h.Core().Conv(ch); ok {
			switch c.Kind {
			case "im", "mpim":
				where = "a direct message"
			default:
				where = "# " + c.Name
			}
		} else {
			r := lb.h.Core().ConvRef(ch)
			switch {
			case r.Kind == "im" || r.Kind == "mpim":
				where = "a direct message"
			case r.Name != "":
				where = "# " + r.Name
			}
		}
		if where != "" {
			if b.Len() > 0 {
				b.WriteString(" ")
			}
			b.WriteString("in ")
			b.WriteString(where)
		}
	}
	if !lb.cur().hosted() {
		return b.String()
	}
	if b.Len() > 0 {
		b.WriteString(" – ")
	}
	b.WriteString(lb.cur().title())
	return b.String()
}

func (lb *Lightbox) footer(gtx layout.Context, th *theme.Theme, panel image.Rectangle) {
	white := rgb(0xffffff)
	hover := rgba(0xffffff, .1)
	y := panel.Max.Y - dp(gtx, 45)
	btn := func(x int, c *widget.Clickable, ic string, tip *TipState, label, sc string) image.Rectangle {
		p := image.Pt(x, y)
		d := at(gtx, p, func(gtx layout.Context) layout.Dimensions {
			d := iconButton(gtx, c, ic, 36, 20, white, hover, 8)
			Tooltip(gtx, th, tip, c.Hovered(), d.Size, Above, label, sc)
			return d
		})
		return image.Rectangle{Min: p, Max: p.Add(d.Size)}
	}
	hosted := lb.cur().hosted()
	x := panel.Min.X + dp(gtx, 12)
	if hosted {
		lb.zoomControls(gtx, btn, x, y)
	}

	// counter
	if n := len(lb.items); n > 1 {
		s := strconv.Itoa(lb.idx+1) + " of " + strconv.Itoa(n)
		w := textW(gtx, th, 13, theme.Bold, s)
		at(gtx, image.Pt(panel.Min.X+(panel.Dx()-w)/2, y), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, dp(gtx, 36), func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Bold, white, s)
			})
		})
	}

	x = panel.Max.X - dp(gtx, 12) - dp(gtx, 36)
	if !hosted {
		// MediaViewerActions without a file: the thread toggle (only
		// with a conversation: toggleThread needs the channel and
		// thread), then Open in new window.
		btn(x, &lb.winBtn, "external", &lb.tips[5], nsViewer.T("Open in new window"), "")
		if lb.m.Channel != "" && lb.m.TS != "" {
			x -= dp(gtx, 48)
			btn(x, &lb.threadBtn, "panel-right", &lb.tips[6], nsViewer.T("Show thread"), "")
		}
		return
	}
	lb.moreRect = btn(x, &lb.moreBtn, "more", &lb.tips[4], nsViewer.T("More actions"), "")
	x -= dp(gtx, 48)
	btn(x, &lb.winBtn, "external", &lb.tips[5], nsViewer.T("Open in new window"), "")
	x -= dp(gtx, 48)
	btn(x, &lb.threadBtn, "panel-right", &lb.tips[6], nsViewer.T("Show thread"), "")
	x -= dp(gtx, 48)
	btn(x, &lb.dlBtn, "download", &lb.tips[7], nsViewer.T("Download"), "")
}

// zoomControls draws Rotate and [− slider +] (and Reset zoom once
// changed) from x on the footer row y.
func (lb *Lightbox) zoomControls(gtx layout.Context, btn func(int, *widget.Clickable, string, *TipState, string, string) image.Rectangle, x, y int) {
	btn(x, &lb.rotBtn, "refresh", &lb.tips[0], nsViewer.T("Rotate"), "")
	x += dp(gtx, 44)
	btn(x, &lb.outBtn, "minus", &lb.tips[1], nsViewer.T("Zoom out"), "-")
	x += dp(gtx, 36)
	// slider
	sw := dp(gtx, lbSlideW)
	sr := image.Rect(x, y, x+sw, y+dp(gtx, 36))
	cy := sr.Min.Y + sr.Dy()/2
	tx0 := sr.Min.X + dp(gtx, lbTrackX0)
	tw := dp(gtx, lbTrackW)
	th2 := maxInt(1, dp(gtx, 2))
	rrect(gtx, image.Rect(tx0, cy-th2/2, tx0+tw, cy-th2/2+th2), th2/2, rgba(0xffffff, .9))
	t := (lb.zoom - lbMinZoom) / (lbMaxZoom - lbMinZoom)
	kx := tx0 + int(t*float32(tw)+.5)
	kd := dp(gtx, 10)
	rrect(gtx, image.Rect(kx-kd/2, cy-kd/2, kx-kd/2+kd, cy-kd/2+kd), kd/2, rgb(0x1d9bd1))
	func() {
		defer op.Offset(sr.Min).Push(gtx.Ops).Pop()
		lbArea(gtx, image.Rectangle{Max: sr.Size()}, &lb.sliderTag, pointer.CursorPointer)
	}()
	x += sw
	btn(x, &lb.inBtn, "plus", &lb.tips[2], nsViewer.T("Zoom in"), "+")
	x += dp(gtx, 44)
	if lb.zoom != 1 || lb.rot != 0 {
		btn(x, &lb.resetBtn, "collapse", &lb.tips[3], nsViewer.T("Reset zoom"), "0")
	}
}

// arrows draws the ‹ › buttons when the message has several images.
func (lb *Lightbox) arrows(gtx layout.Context, panel image.Rectangle) {
	if len(lb.items) < 2 {
		return
	}
	sz := dp(gtx, 40)
	y := panel.Min.Y + (panel.Dy()-sz)/2
	draw := func(x int, c *widget.Clickable, ic string) {
		at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, c, func(gtx layout.Context, s image.Point, hovered bool) {
				a := float32(.5)
				if hovered {
					a = .75
				}
				rrect(gtx, image.Rectangle{Max: s}, s.X/2, rgba(0x000000, a))
			}, func(gtx layout.Context) layout.Dimensions {
				off := (sz - dp(gtx, 20)) / 2
				at(gtx, image.Pt(off, off), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, ic, 20, rgb(0xffffff))
				})
				return layout.Dimensions{Size: image.Pt(sz, sz)}
			})
		})
	}
	if lb.idx > 0 {
		draw(panel.Min.X+dp(gtx, 12), &lb.prevBtn, "chevron-left")
	}
	if lb.idx < len(lb.items)-1 {
		draw(panel.Max.X-dp(gtx, 12)-sz, &lb.nextBtn, "chevron-right")
	}
}

// ---- download / open ----

// download saves the original image bytes to the user's Downloads folder
// (an explicit user action). Slack-hosted files come from core.Fetch.
func (lb *Lightbox) download() {
	if lb.busy {
		return
	}
	h := lb.h
	it := lb.cur()
	src := it.img.URL
	name := ""
	if f := it.file; f != nil {
		if f.URL != "" {
			src = f.URL
		} else if f.Download != "" {
			src = f.Download
		}
		name = f.Name
	}
	if name == "" {
		name = it.img.Title
	}
	lb.busy = true
	saveDownload(h, src, name, it.img.Proxy || it.file != nil, func() { lb.busy = false })
}

// lbSlackHost reports whether raw is on Slack's own hosts, which core.Fetch
// handles (with the cookie only for slack.com).
func lbSlackHost(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && slack.SlackOwnedHost(u.Hostname())
}

// lbGet downloads a public (non-Slack) image with no credentials:
// imgcache.PublicFetchLimit refuses non-https, userinfo and private
// addresses, then loads it through Slack's image proxy, so saving an unfurl
// image doesn't reveal your IP address to its host. At most core.MaxFetch.
func lbGet(ctx context.Context, raw string) ([]byte, error) {
	return imgcache.PublicFetchLimit(ctx, raw, core.MaxFetch)
}

// Download location (Preferences > Advanced, the ui root sets these):
// DownloadDir is the folder downloads go to ("" for ~/Downloads), and
// AskDownload, when set and on, saves through the system's save dialog
// instead (Slack's "Choose location for each download"). Both run off
// the UI goroutine.
var (
	DownloadDir func() string
	AskDownload func(name string, data []byte) (path string, asked bool, err error)
)

// ErrDownloadCanceled is a save dialog the user closed: no toast.
var ErrDownloadCanceled = errors.New("download canceled")

// lbSave writes data to the download folder under a unique name (or
// where the save dialog says) and returns the path.
func lbSave(name, src string, data []byte) (string, error) {
	if AskDownload != nil {
		if p, asked, err := AskDownload(lbFileName(name, src, data), data); asked {
			return p, err
		}
	}
	dir := ""
	if DownloadDir != nil {
		dir = DownloadDir()
	}
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, "Downloads")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return lbSaveIn(dir, name, src, data)
}

// lbSaveIn writes data to a new file in dir. The Slack-supplied name is
// reduced to one plain file name (no separators, "..", drive letters or
// Windows device names), and O_EXCL means an existing file or symlink is
// never overwritten or followed.
func lbSaveIn(dir, name, src string, data []byte) (string, error) {
	name = lbFileName(name, src, data)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; i < 1000; i++ {
		fn := name
		if i > 0 {
			fn = stem + " (" + strconv.Itoa(i) + ")" + ext
		}
		p := filepath.Join(dir, fn)
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			os.Remove(p)
			return "", err
		}
		if err := f.Close(); err != nil {
			return "", err
		}
		return p, nil
	}
	return "", errors.New("no free file name")
}

// lbFileName is the name a download saves as: the Slack-supplied name
// reduced to one plain file name (lbCleanName), else the URL's last
// segment, else "image", with an extension from the bytes when it has
// none.
func lbFileName(name, src string, data []byte) string {
	name = lbCleanName(name)
	if name == "" {
		if u, err := url.Parse(src); err == nil {
			name = lbCleanName(path.Base(u.Path))
		}
	}
	if name == "" {
		name = "image"
	}
	ext := filepath.Ext(name)
	if ext == "" {
		switch http.DetectContentType(data) {
		case "image/png":
			ext = ".png"
		case "image/jpeg":
			ext = ".jpg"
		case "image/gif":
			ext = ".gif"
		case "image/webp":
			ext = ".webp"
		}
		name += ext
	}
	return name
}

// lbCleanName makes name safe as a single file name.
func lbCleanName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20, r == 0x7f, strings.ContainsRune(`/\:*?"<>|`, r):
			return '_'
		case r == 0x200e, r == 0x200f, r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
			return '_' // bidi controls: "\u202egnp.exe" would display as "exe.png"
		}
		return r
	}, strings.TrimSpace(name))
	name = strings.Trim(name, ". ")
	if len(name) > 200 {
		name = strings.ToValidUTF8(name[:200], "")
		name = strings.TrimRight(name, ". ")
	}
	// Windows device names (CON, NUL, COM1, CONIN$, …) open the device, not
	// a file, whatever the extension.
	stem := strings.ToUpper(name)
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	stem = strings.TrimRight(stem, " ")
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		name = "_" + name
	default:
		if len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '0' && stem[3] <= '9' {
			name = "_" + name
		}
	}
	return name
}

// lbOpen opens an http(s) URL in the system browser.
func lbOpen(h Host, raw string) {
	if !strings.HasPrefix(raw, "https://") && !strings.HasPrefix(raw, "http://") {
		h.Toast("Can't open this link")
		return
	}
	if err := openurl.Open(raw); err != nil {
		h.Toast("Couldn't open the browser: " + errText(err))
	}
}

func lbMaxF(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func lbClampF(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
