package richview

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gioui.org/f32"
	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// View lays out and draws one Doc. Keep one View per message and reuse it
// across frames; the layout is cached until the doc, the width, the scale
// or the Style generation changes.
type View struct {
	doc  richtext.Doc
	opts Options

	d       display
	laidW   int
	laidPx  float32
	laidSp  float32
	laidGen uint32
	laidEl  uint32 // Options.Elements version the layout was made with
	laidPf  uint32 // uipref.Gen the layout was made with (jumbomoji, swatches, link underlines, inline media)
	dirty   bool

	ops      op.Ops
	call     op.CallOp
	recorded bool
	pending  []int32 // imgs (>=0) or ^frag index of images not yet decoded
	live     []int32 // frags of animated emoji: drawn every frame, not recorded

	// Slack components (comps.go): the mode the layout was made in, each
	// component's state and its props/natives receiver.
	laidMode uint8
	cs       []compState
	refs     []compRef

	// css: plugin/user CSS on the body's parts (css.go); empty while no
	// CSS is installed.
	css cssParts

	hover    int16 // hovered target
	press    int16 // target under the press
	pressPos f32.Point
	moved    bool
	dragging bool
	selA     int32
	selB     int32
	focused  bool
	lastTap  time.Duration
	tapCount int
	// clicks on a link target sent to the DOM (clickLink): its click count
	clickT   int16
	clickAt  time.Duration
	clickN   int
	hovering bool
	ptr      f32.Point  // last pointer position (view coordinates)
	tipFrag  int        // emoji frag under the pointer, -1 none
	tipAt    time.Time  // when the pointer reached it (gtx.Now)
	hovMen   mentionKey // the @mention last reported to the hover card
	hovOn    bool
	// the link preview whose hover trigger was last reported, and each
	// preview's hover card content (unfurl_hover.go)
	prevKey  previewKey
	prevOn   bool
	previews map[*richtext.Attachment]*Preview

	// code blocks' copy buttons (codecopy.go): run index+1 under the
	// press, the run last copied and when, and when the pointer reached a
	// button (its tooltip's delay)
	copyPress int
	copied    int
	copiedAt  time.Time
	copyTipAt time.Time
	copyHot   int

	// Origin, if set by the owner, is added to click anchors so they are in
	// window coordinates.
	Origin image.Point

	// expanded: attachments whose collapsed text was expanded (truncate.go)
	expanded map[*richtext.Attachment]bool
	// collapsed: image blocks hidden with their title-row toggle
	collapsed map[*richtext.Image]bool
	redraw    bool // ask for a frame after this frame's events

	// lead: the first line's start (SetLead)
	lead int

	// External hands text selection to the owner (cross-message selection
	// in a list): the View still handles clicks and hover but never changes
	// its selection or takes focus; the owner calls SetSelection.
	External bool
}

// OffsetAt maps a point in View coordinates to a plain-text offset (valid
// after the first Layout).
func (v *View) OffsetAt(p f32.Point) int32 { return v.offsetAt(p) }

// PlainLen is the length of the selectable plain text.
func (v *View) PlainLen() int32 { return int32(len(v.d.plain)) }

// TextRange returns plain text [a, b) (clamped).
func (v *View) TextRange(a, b int32) string {
	n := int32(len(v.d.plain))
	a, b = min(max(a, 0), n), min(max(b, 0), n)
	if a >= b {
		return ""
	}
	return string(v.d.plain[a:b])
}

// SetSelection sets the selected range (External mode).
func (v *View) SetSelection(a, b int32) {
	n := int32(len(v.d.plain))
	v.selA, v.selB = min(max(a, 0), n), min(max(b, 0), n)
}

// WordAt returns the word range around off (double-click).
func (v *View) WordAt(off int32) (int32, int32) {
	sa, sb := v.selA, v.selB
	v.selectWord(off)
	a, b := v.selA, v.selB
	v.selA, v.selB = sa, sb
	return a, b
}

// LineAt returns the line (paragraph) range around off (triple-click).
func (v *View) LineAt(off int32) (int32, int32) {
	sa, sb := v.selA, v.selB
	v.selectLine(off)
	a, b := v.selA, v.selB
	v.selA, v.selB = sa, sb
	return a, b
}

// New returns a View for doc.
func New(doc richtext.Doc, o Options) *View {
	v := &View{hover: -1, press: -1, tipFrag: -1}
	v.Set(doc, o)
	return v
}

// Set replaces the document. Call it only when the message changed.
func (v *View) Set(doc richtext.Doc, o Options) {
	v.doc = doc
	v.opts = o
	v.dirty = true
	v.cs = v.cs[:0]
	v.selA, v.selB = 0, 0
	v.hover, v.press = -1, -1
	v.tipFrag = -1
}

// SetOptions changes only the options (e.g. edited flag, pending state).
func (v *View) SetOptions(o Options) {
	if o != v.opts {
		v.opts = o
		v.dirty = true
	}
}

// SetLead starts the first line of the doc's first paragraph px in, the
// rest of it wrapping at the left edge: the message's sender sits inline
// before it in Compact (Message.getCompactMessageBody: the sender, a
// no-break space, then .p-rich_text_block--inline). A doc that opens with
// anything else (a list, a quote, code, a card) ignores it.
func (v *View) SetLead(px int) {
	px = max(px, 0)
	if px != v.lead {
		v.lead = px
		v.dirty = true
	}
}

// Lead is the first line's start SetLead gave (px).
func (v *View) Lead() int { return v.lead }

// Options returns the current options.
func (v *View) Options() Options { return v.opts }

// Doc returns the current document.
func (v *View) Doc() richtext.Doc { return v.doc }

// Release drops the layout and its recorded paint ops (glyph paths, the
// bulk of a message's memory). The next Layout lays the doc out again;
// state (selection, component states) is kept. For rows that have been
// off screen for a while.
func (v *View) Release() {
	if !v.recorded && len(v.d.frags) == 0 {
		return
	}
	v.d = display{}
	v.ops = op.Ops{}
	v.call = op.CallOp{}
	v.recorded = false
	v.pending = nil
	v.dirty = true
}

// HasSelection reports whether some text is selected.
func (v *View) HasSelection() bool { return v.selA != v.selB }

// ClearSelection drops the selection.
func (v *View) ClearSelection() { v.selA, v.selB = 0, 0 }

// SelectedText returns the selected text.
func (v *View) SelectedText() string {
	a, b := v.selRange()
	if a == b || int(b) > len(v.d.plain) {
		return ""
	}
	return string(v.d.plain[a:b])
}

// Hovered reports whether the pointer is over a clickable item.
func (v *View) Hovered() bool { return v.hover >= 0 }

// Pointer reports whether the pointer is inside the view.
func (v *View) Pointer() bool { return v.hovering }

func (v *View) selRange() (int32, int32) {
	if v.selA <= v.selB {
		return v.selA, v.selB
	}
	return v.selB, v.selA
}

// Layout draws the doc at the constraints' max width and returns its size.
func (v *View) Layout(gtx layout.Context, st *Style, h Handler) layout.Dimensions {
	w := gtx.Constraints.Max.X
	if v.opts.MaxWidth > 0 {
		if mw := gtx.Dp(dpUnit(v.opts.MaxWidth)); mw < w {
			w = mw
		}
	}
	if w <= 0 {
		w = 1
	}
	mode := compMode()
	if el := v.opts.Elements; el != nil && el.ElementsVersion() != v.laidEl {
		v.dirty = true
	}
	if relay, rec := v.css.sync(slackc.CurrentScope()); relay {
		v.dirty = true
	} else if rec {
		v.recorded = false
	}
	if v.namesChanged() {
		v.dirty = true // a mention's plugin label changed (members.go)
	}
	if v.dirty || w != v.laidW || gtx.Metric.PxPerDp != v.laidPx || gtx.Metric.PxPerSp != v.laidSp || st.gen != v.laidGen || mode != v.laidMode || uipref.Gen() != v.laidPf {
		v.relayout(st, w, gtx.Metric.PxPerDp, gtx.Metric.PxPerSp)
	} else if len(v.pending) > 0 && v.anyReady(st) {
		v.recorded = false
	}
	if !v.recorded {
		v.record(st)
	}
	v.events(gtx, st, h)
	v.hoverCard(st, h)

	// Slack's .p-rich_text_block > .p-rich_text_section, for CSS.
	bs := richBlockClass.Enter(gtx, 0, 0)
	defer bs.Leave()
	ss := richSectionClass.Enter(gtx, 0, 0)
	defer ss.Leave()
	if bs.Hidden() || ss.Hidden() {
		return layout.Dimensions{}
	}
	if bs.Active() || ss.Active() {
		return bs.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
			return ss.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return v.draw(gtx, st) })
		})
	}
	return v.draw(gtx, st)
}

// draw paints the laid-out doc and registers its input areas.
func (v *View) draw(gtx layout.Context, st *Style) layout.Dimensions {
	size := v.d.size
	if gtx.Constraints.Min.X > size.X {
		size.X = gtx.Constraints.Min.X
	}
	// the v1/v2 remove buttons' rail sits left of the View's box
	v.drawRails(gtx, st)
	v.railInput(gtx)
	defer clip.Rect{Min: image.Pt(-v.d.overhang, 0), Max: size}.Push(gtx.Ops).Pop()
	var opac paint.OpacityStack
	if v.opts.Dim {
		opac = paint.PushOpacity(gtx.Ops, 0.5)
	}
	if v.selA != v.selB {
		v.paintSelection(gtx, st)
	}
	// Hover backgrounds for pills, buttons and cards draw under the content.
	if v.hover >= 0 {
		for i := range v.d.boxes {
			b := &v.d.boxes[i]
			if (b.target == v.hover || b.tgTo > b.target && v.hover > b.target && v.hover <= b.tgTo) && b.hover.A > 0 {
				switch b.kind {
				case bBorder:
					// Card hover: stronger border on top.
					strokeRRect(gtx.Ops, b.r, b.radius, b.bw, b.hover)
				default:
					paint.FillShape(gtx.Ops, b.hover, clip.UniformRRect(b.r, b.radius).Op(gtx.Ops))
				}
			}
		}
	}
	v.call.Add(gtx.Ops)
	for _, i := range v.live {
		if f := &v.d.frags[i]; st.Images != nil {
			if img, ok := st.Images.Get(f.url, f.size); ok {
				r := image.Rect(int(f.x+0.5), f.y, int(f.x+0.5)+f.size, f.y+f.size)
				if ps := v.css.get(f.part); ps != nil && ps.alpha < 1 { // CSS opacity on the emoji (css.go)
					o := paint.PushOpacity(gtx.Ops, max(ps.alpha, 0))
					drawImage(gtx.Ops, img, r, [4]int{}, false)
					o.Pop()
				} else {
					drawImage(gtx.Ops, img, r, [4]int{}, false)
				}
			}
		}
	}
	for i := range v.d.spinners {
		v.d.spinners[i].draw(gtx, st)
	}
	if len(v.d.comps) > 0 {
		v.layoutComps(gtx, st)
	}
	// a hovered message_mention pill's text and icon take Slack's hover
	// color (.p-rich_text_slug:hover), over the recorded ones
	if v.hover >= 0 && int(v.hover) < len(v.d.targets) && v.d.targets[v.hover].slug && st.Palette.SlugHoverText.A > 0 {
		col := st.Palette.SlugHoverText
		for i := range v.d.frags {
			f := &v.d.frags[i]
			if f.target != v.hover {
				continue
			}
			switch f.kind {
			case fText:
				if f.sh != nil && len(f.sh.glyphs) > 0 {
					v.paintGlyphs(gtx.Ops, st, f, col)
				}
			case fImage:
				if name, ok := strings.CutPrefix(f.url, iconURL); ok {
					g := *f
					g.col = col
					paintIcon(gtx.Ops, name, &g)
				}
			}
		}
	}
	if v.hover >= 0 && int(v.hover) < len(v.d.targets) {
		switch tg := &v.d.targets[v.hover]; {
		case tg.inset:
			v.insetLine(gtx.Ops, v.hover, max(1, gtx.Dp(1)))
		case tg.underline:
			for i := range v.d.frags {
				f := &v.d.frags[i]
				if f.target == v.hover && f.kind == fText {
					underline(gtx.Ops, f, f.w+f.ulT, v.textCol(f))
				}
			}
		}
	}
	v.drawCopyButtons(gtx, st)
	v.drawRemoves(gtx, st)
	if v.opts.Dim {
		opac.Pop()
	}
	v.emojiTip(gtx, st)

	// Input areas: the whole view takes text-selection input; clickable
	// things show the hand cursor.
	event.Op(gtx.Ops, v)
	pointer.CursorText.Add(gtx.Ops)
	for i := range v.d.hits {
		hr := &v.d.hits[i]
		if hr.r.Empty() {
			continue
		}
		s := clip.Rect(hr.r).Push(gtx.Ops)
		pointer.CursorPointer.Add(gtx.Ops)
		s.Pop()
	}
	for i := range v.d.codes {
		if c := &v.d.codes[i]; c.block {
			s := clip.Rect(v.copyButton(c)).Push(gtx.Ops)
			pointer.CursorPointer.Add(gtx.Ops)
			s.Pop()
		}
	}
	for i := range v.d.frags {
		f := &v.d.frags[i]
		if f.target >= 0 {
			s := clip.Rect(fragRect(f)).Push(gtx.Ops)
			pointer.CursorPointer.Add(gtx.Ops)
			s.Pop()
		}
	}
	// Element runs (Options.Elements) go last: their input areas must be
	// above the cursor/hit areas of cards and links registered above.
	if el := v.opts.Elements; el != nil && len(v.d.slots) > 0 {
		var opac paint.OpacityStack
		if v.opts.Dim {
			opac = paint.PushOpacity(gtx.Ops, 0.5)
		}
		for i := range v.d.slots {
			s := &v.d.slots[i]
			off := op.Offset(s.r.Min).Push(gtx.Ops)
			g := gtx
			g.Constraints = layout.Exact(s.r.Size())
			el.LayoutElements(g, s.bs, s.acc)
			off.Pop()
		}
		if v.opts.Dim {
			opac.Pop()
		}
	}
	d := layout.Dimensions{Size: size}
	if b := v.d.firstBaseline(); b >= 0 && b <= size.Y {
		d.Baseline = size.Y - b
	}
	return d
}

// firstBaseline is the first line's baseline from the top (px), the way
// a browser aligns inline content: text sits on its baseline, an
// image-only line (emoji) on the image's bottom edge. -1 when nothing
// was laid out. Hosts that put the doc inline (MrkdwnElement in a plugin
// tree) align it to the surrounding text with it.
func (d *display) firstBaseline() int {
	if len(d.lines) == 0 {
		return -1
	}
	ln := d.lines[0]
	base, img := -1, -1
	for i := ln.f0; i < ln.f1 && i < len(d.frags); i++ {
		f := &d.frags[i]
		switch f.kind {
		case fText:
			base = max(base, f.y)
		case fImage:
			img = max(img, f.y+f.size)
		}
	}
	if base >= 0 {
		return base
	}
	return img
}

func fragRect(f *frag) image.Rectangle {
	return image.Rect(int(f.x), f.top, int(f.x+f.w+0.999), f.bot)
}

func (v *View) relayout(st *Style, w int, pxdp, pxsp float32) {
	mode := compMode()
	if el := v.opts.Elements; el != nil {
		v.laidEl = el.ElementsVersion()
	}
	v.d.reset()
	v.laidPf = uipref.Gen()
	v.tipFrag = -1
	if v.expanded == nil {
		v.expanded = map[*richtext.Attachment]bool{}
	}
	if v.collapsed == nil {
		v.collapsed = map[*richtext.Image]bool{}
	}
	// JUMBOMOJI is off under Compact (client-boot Y9zX: jumbomoji &&
	// messages_theme != "dense")
	pf := uipref.Get()
	l := lay{st: st, d: &v.d, scale: pxdp, spx: pxsp, jumbo: v.doc.Jumbo && pf.Jumbomoji && !pf.Dense, lead: float32(v.lead), opts: v.opts, mode: mode, cs: &v.cs,
		expanded: v.expanded, collapsed: v.collapsed}
	v.css.parts = v.css.parts[:0]
	if v.css.scope.Active() {
		l.css = &v.css
		l.curPart = l.partFor(&l.ctr)
	}
	if l.scale <= 0 {
		l.scale = 1
	}
	if l.spx <= 0 {
		l.spx = l.scale
	}
	var h int
	if v.opts.HoverCard {
		h = l.hoverCardDoc(v.doc.Blocks, w)
	} else {
		h = l.blocks(v.doc.Blocks, 0, 0, w, true)
	}
	v.d.pillRects()
	maxX := 0
	for i := range v.d.frags {
		if x := int(v.d.frags[i].x + v.d.frags[i].w + 0.999); x > maxX {
			maxX = x
		}
	}
	for i := range v.d.boxes {
		if x := v.d.boxes[i].r.Max.X; x > maxX {
			maxX = x
		}
		if y := v.d.boxes[i].r.Max.Y; y > h {
			h = y
		}
	}
	for i := range v.d.imgs {
		if x := v.d.imgs[i].r.Max.X; x > maxX {
			maxX = x
		}
		if y := v.d.imgs[i].r.Max.Y; y > h {
			h = y
		}
	}
	for i := range v.d.removes { // a remove button sits 9px past its card
		if x := v.d.removes[i].r.Max.X; x > maxX {
			maxX = x
		}
	}
	if maxX > w {
		maxX = w
	}
	v.d.size = image.Pt(maxX, h)
	v.laidW, v.laidPx, v.laidSp, v.laidGen, v.laidMode = w, pxdp, pxsp, st.gen, mode
	v.dirty = false
	v.recorded = false
	if int(v.selB) > len(v.d.plain) || int(v.selA) > len(v.d.plain) {
		v.selA, v.selB = 0, 0
	}
	if int(v.hover) >= len(v.d.targets) {
		v.hover = -1
	}
}

func (v *View) anyReady(st *Style) bool {
	if st.Images == nil {
		return false
	}
	for _, p := range v.pending {
		if p >= 0 {
			im := &v.d.imgs[p]
			if _, ok := st.Images.Get(im.url, im.px); ok {
				return true
			}
		} else {
			f := &v.d.frags[^p]
			if _, ok := st.Images.Get(f.url, f.size); ok {
				return true
			}
		}
	}
	return false
}

// record paints the static content into v.ops.
func (v *View) record(st *Style) {
	v.ops.Reset()
	v.pending = v.pending[:0]
	v.live = v.live[:0]
	ops := &v.ops
	m := op.Record(ops)
	v.paint(ops, st, 0, nil)
	v.call = m.Stop()
	v.recorded = true
}

// paint draws the primitives tagged comp (0: the static content, which
// also tracks images still loading); fg, if set, recolors text.
func (v *View) paint(ops *op.Ops, st *Style, comp int16, fg *color.NRGBA) {
	css := len(v.css.parts) > 0
	for i := range v.d.boxes {
		if b := &v.d.boxes[i]; b.layer == 0 && b.comp == comp {
			v.drawBox(ops, b, css)
		}
	}
	if css {
		v.partBackgrounds(func(r image.Rectangle, c color.NRGBA) { fillRadii(ops, r, [4]int{}, c) }, comp)
	}
	for i := range v.d.imgs {
		im := &v.d.imgs[i]
		if im.comp != comp {
			continue
		}
		if im.url == "" {
			fillRadii(ops, im.r, im.radii, st.Palette.Placeholder)
			continue
		}
		if name, ok := strings.CutPrefix(im.url, iconURL); ok { // a vector icon (unfurl footers, placeholders)
			paintIconAt(ops, name, im.r, im.tint)
			continue
		}
		var img paint.ImageOp
		ok := false
		if st.Images != nil {
			img, ok = st.Images.Get(im.url, im.px)
		}
		if !ok {
			fillRadii(ops, im.r, im.radii, st.Palette.Placeholder)
			if comp == 0 {
				v.pending = append(v.pending, int32(i))
			}
			continue
		}
		if im.fit {
			fillRadii(ops, im.r, im.radii, st.Palette.Placeholder)
		}
		if im.alpha > 0 && im.alpha < 1 {
			o := paint.PushOpacity(ops, im.alpha)
			drawImage(ops, img, im.r, im.radii, !im.fit)
			o.Pop()
			continue
		}
		drawImage(ops, img, im.r, im.radii, !im.fit)
	}
	for i := range v.d.boxes { // over images, under text (gallery scrims, video pills)
		if b := &v.d.boxes[i]; b.layer == 2 && b.comp == comp {
			v.drawBox(ops, b, css)
		}
	}
	if comp == 0 {
		for i := range v.d.overlayIcons {
			o := &v.d.overlayIcons[i]
			paintIconAt(ops, o.name, o.r, o.col)
		}
	}
	ul := uipref.Get().UnderlineLinks
	for i := range v.d.frags {
		f := &v.d.frags[i]
		if f.comp != comp {
			continue
		}
		switch f.kind {
		case fImage:
			if name, ok := strings.CutPrefix(f.url, iconURL); ok { // a vector icon (pills)
				paintIcon(ops, name, f)
				continue
			}
			if comp == 0 && animated(st.Images, f.url, f.size) {
				v.live = append(v.live, int32(i)) // a recording would freeze it
				continue
			}
			var img paint.ImageOp
			ok := false
			if st.Images != nil {
				img, ok = st.Images.Get(f.url, f.size)
			}
			if !ok {
				if comp == 0 {
					v.pending = append(v.pending, ^int32(i))
				}
				continue
			}
			r := image.Rect(int(f.x+0.5), f.y, int(f.x+0.5)+f.size, f.y+f.size)
			if ps := v.css.get(f.part); ps != nil && ps.alpha < 1 {
				o := paint.PushOpacity(ops, max(ps.alpha, 0))
				drawImage(ops, img, r, [4]int{}, false)
				o.Pop()
				continue
			}
			drawImage(ops, img, r, [4]int{}, false)
		case fText:
			if f.sh == nil || len(f.sh.glyphs) == 0 {
				continue
			}
			col := f.col
			if fg != nil {
				col = *fg
			} else if css && f.part != 0 {
				col = v.textCol(f)
			}
			v.paintGlyphs(ops, st, f, col)
			if f.style&(richtext.Strike|richtext.Underline) != 0 {
				thick := max(1, f.size/14)
				w := f.w + f.ulU // over the spaces of the same style
				if f.style&richtext.Strike != 0 {
					y := f.y - int(float32(f.size)*0.3+0.5)
					paint.FillShape(ops, col, clip.Rect(image.Rect(int(f.x), y, int(f.x+w+0.5), y+thick)).Op())
				}
				if f.style&richtext.Underline != 0 {
					underline(ops, f, w, col)
				}
			} else if ul && f.target >= 0 && int(f.target) < len(v.d.targets) && v.d.targets[f.target].website {
				underline(ops, f, f.w+f.ulT, col) // Accessibility > "Underline links to websites"
			}
			if css && f.part != 0 {
				v.partDeco(ops, i, col)
			}
		}
	}
	for i := range v.d.boxes {
		if b := &v.d.boxes[i]; b.layer == 1 && b.comp == comp {
			v.drawBox(ops, b, css)
		}
	}
}

// drawBox draws b, with its CSS part's fill when it has one.
func (v *View) drawBox(ops *op.Ops, b *box, css bool) {
	if !css || b.part == 0 {
		drawBox(ops, b)
		return
	}
	bb := *b
	bb.fill = v.boxFill(b)
	if ps := v.css.get(b.part); ps != nil {
		bb.stroke = mulAlpha(bb.stroke, ps.alpha)
	}
	drawBox(ops, &bb)
}

// partDeco draws the text-decoration CSS gives frag i's part (its own or
// propagated from the row), joined over the space to the next word of
// the same part on the line.
func (v *View) partDeco(ops *op.Ops, i int, col color.NRGBA) {
	f := &v.d.frags[i]
	ps := v.css.get(f.part)
	if ps == nil || ps.deco.Lines() == 0 {
		return
	}
	w := f.w
	if j := i + 1; j < len(v.d.frags) {
		if n := &v.d.frags[j]; n.kind == fText && n.line == f.line && n.part == f.part && n.x > f.x {
			w = n.x - f.x
		}
	}
	dc := col
	if ps.decoCol.A > 0 {
		dc = mulAlpha(ps.decoCol, ps.alpha)
	}
	slackc.DrawDecoration(ops, ps.deco, dc, f.x, float32(f.y), w, float32(f.size))
}

// underline draws f's underline w px long (the word, plus the spaces
// after it its decoration covers: frag.ulT, ulU) in col, its painted (CSS)
// color.
func underline(ops *op.Ops, f *frag, w float32, col color.NRGBA) {
	thick := max(1, f.size/14)
	y := f.y + max(1, f.size/8)
	paint.FillShape(ops, col, clip.Rect(image.Rect(int(f.x), y, int(f.x+w+0.5), y+thick)).Op())
}

// insetLine is a hovered target's box-shadow: inset 0 -1px: a thick px
// line on the bottom edge of each of its inline boxes (baseline plus the
// font's descent), from its first frag on the line to its last, icons
// included, in the text's color.
func (v *View) insetLine(ops *op.Ops, t int16, thick int) {
	var x0, x1 float32
	y, on := 0, false
	var col color.NRGBA
	line := int32(-1)
	flush := func() {
		if on && y > 0 && x1 > x0 {
			paint.FillShape(ops, col, clip.Rect(image.Rect(int(x0+0.5), y-thick, int(x1+0.5), y)).Op())
		}
		on, y = false, 0
	}
	for i := range v.d.frags {
		f := &v.d.frags[i]
		if f.target != t || f.line < 0 {
			if on && f.line == line {
				flush() // another element between: a new box
			}
			continue
		}
		if !on || f.line != line {
			flush()
			on, line, x0, x1 = true, f.line, f.x, f.x+f.w
		}
		x0, x1 = min(x0, f.x), max(x1, f.x+f.w)
		if y == 0 && f.kind == fText && f.sh != nil {
			y = f.y + int(f.sh.desc+0.5)
			col = v.textCol(f)
		}
	}
	flush()
}

func (v *View) paintSelection(gtx layout.Context, st *Style) {
	a, b := v.selRange()
	col := st.Palette.Selection
	fr := v.d.frags
	for i := range fr {
		f := &fr[i]
		if f.line < 0 || f.p1 <= a || f.p0 >= b || f.p0 == f.p1 {
			continue
		}
		x0, x1 := f.x, f.x+f.w
		if f.kind == fText && f.sh != nil {
			if a > f.p0 {
				x0 = f.x + f.sh.caretX(runesIn(f.sh.str, int(a-f.p0)))
			}
			if b < f.p1 {
				x1 = f.x + f.sh.caretX(runesIn(f.sh.str, int(b-f.p0)))
			}
		}
		// Extend over the gap to the next selected frag on the same line.
		if b >= f.p1 && i+1 < len(fr) && fr[i+1].line == f.line && fr[i+1].p0 < b && fr[i+1].x > x1 {
			x1 = fr[i+1].x
		}
		// Round both edges the same way so adjacent rects never overlap
		// (overlapping translucent fills show as darker seams).
		r := image.Rect(int(x0+0.5), f.top, int(x1+0.5), f.bot)
		paint.FillShape(gtx.Ops, col, clip.Rect(r).Op())
	}
}

// runesIn counts the runes in the first n bytes of s.
func runesIn(s string, n int) int {
	if n > len(s) {
		n = len(s)
	}
	return utf8.RuneCountInString(s[:n])
}

// offsetAt maps a point to a plain-text offset.
func (v *View) offsetAt(p f32.Point) int32 {
	lines := v.d.lines
	if len(lines) == 0 {
		return 0
	}
	x, y := p.X, int(p.Y)
	best := -1
	bestD := 1 << 30
	for i, ln := range lines {
		if ln.f1 <= ln.f0 {
			continue
		}
		var dy int
		switch {
		case y < ln.top:
			dy = ln.top - y
		case y >= ln.bot:
			dy = y - ln.bot + 1
		}
		// Prefer lines whose horizontal span contains x (side-by-side fields).
		f0, f1 := &v.d.frags[ln.f0], &v.d.frags[ln.f1-1]
		dx := 0
		if x < f0.x {
			dx = int(f0.x - x)
		} else if x > f1.x+f1.w {
			dx = int(x - f1.x - f1.w)
		}
		d := dy*4 + dx/8
		if dy == 0 {
			d = dx / 8
		}
		if d < bestD {
			bestD, best = d, i
		}
	}
	if best < 0 {
		return 0
	}
	ln := lines[best]
	if y < ln.top && best == 0 {
		return v.d.frags[ln.f0].p0
	}
	for fi := ln.f0; fi < ln.f1; fi++ {
		f := &v.d.frags[fi]
		if f.p0 < 0 {
			continue
		}
		if x < f.x {
			return f.p0
		}
		if x <= f.x+f.w {
			if f.kind == fText && f.sh != nil {
				r := f.sh.runeAt(x - f.x)
				return min(f.p1, f.p0+int32(bytesForRunes(f.sh.str, r)))
			}
			if x < f.x+f.w/2 {
				return f.p0
			}
			return f.p1
		}
	}
	return v.d.frags[ln.f1-1].p1
}

func bytesForRunes(s string, r int) int {
	b := 0
	for i := 0; i < r && b < len(s); i++ {
		_, sz := utf8.DecodeRuneInString(s[b:])
		b += sz
	}
	return b
}

// targetAt returns the clickable target under p.
func (v *View) targetAt(p f32.Point) int16 {
	pt := image.Pt(int(p.X), int(p.Y))
	for i := range v.d.frags {
		f := &v.d.frags[i]
		if f.target >= 0 && pt.In(fragRect(f)) {
			return f.target
		}
	}
	// Later hits are on top (cards contain their images).
	for i := len(v.d.hits) - 1; i >= 0; i-- {
		if pt.In(v.d.hits[i].r) {
			return v.d.hits[i].t
		}
	}
	return -1
}

func (v *View) selectWord(off int32) {
	pl := v.d.plain
	off = min(max(off, 0), int32(len(pl)))
	a, b := int(off), int(off)
	isW := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '\'' }
	for a > 0 {
		r, sz := utf8.DecodeLastRune(pl[:a])
		if !isW(r) {
			break
		}
		a -= sz
	}
	for b < len(pl) {
		r, sz := utf8.DecodeRune(pl[b:])
		if !isW(r) {
			break
		}
		b += sz
	}
	v.selA, v.selB = int32(a), int32(b)
}

func (v *View) selectLine(off int32) {
	pl := v.d.plain
	off = min(max(off, 0), int32(len(pl)))
	if a, b, ok := v.codeRange(off); ok { // selectContentsOnTripleMouseDown
		v.selA, v.selB = a, b
		return
	}
	a, b := int(off), int(off)
	for a > 0 && pl[a-1] != '\n' {
		a--
	}
	for b < len(pl) && pl[b] != '\n' {
		b++
	}
	v.selA, v.selB = int32(a), int32(b)
}

func (v *View) events(gtx layout.Context, st *Style, h Handler) {
	defer func() {
		if v.redraw { // a click changed the layout (Show more): next frame now
			v.redraw = false
			gtx.Execute(op.InvalidateCmd{})
		}
	}()
	for {
		ev, ok := gtx.Event(
			pointer.Filter{Target: v, Kinds: pointer.Press | pointer.Release | pointer.Drag | pointer.Move | pointer.Enter | pointer.Leave | pointer.Cancel},
			key.FocusFilter{Target: v},
			key.Filter{Focus: v, Name: "C", Required: key.ModShortcut},
			key.Filter{Focus: v, Name: "A", Required: key.ModShortcut},
		)
		if !ok {
			break
		}
		switch e := ev.(type) {
		case key.FocusEvent:
			v.focused = e.Focus
			if !e.Focus && !v.dragging && !v.External {
				v.selA, v.selB = 0, 0
			}
		case key.Event:
			if e.State != key.Press {
				continue
			}
			switch e.Name {
			case "C":
				if s := v.SelectedText(); s != "" {
					gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(s))})
				}
			case "A":
				v.selA, v.selB = 0, int32(len(v.d.plain))
			}
		case pointer.Event:
			switch e.Kind {
			case pointer.Enter, pointer.Move:
				v.hovering = true
				v.hover = v.targetAt(e.Position)
				v.ptr = e.Position
				if b := v.copyButtonAt(e.Position) + 1; b != v.copyHot {
					v.copyHot, v.copyTipAt = b, gtx.Now
				}
				if f := v.emojiAt(e.Position); f != v.tipFrag {
					v.tipFrag, v.tipAt = f, gtx.Now
				}
			case pointer.Leave:
				v.hovering = false
				v.tipFrag = -1
				if !v.dragging {
					v.hover = -1
				}
			case pointer.Cancel:
				v.dragging = false
				v.press = -1
				v.copyPress = 0
			case pointer.Press:
				if !ctxclick.Primary(e) || v.inSlot(e.Position) {
					// element widgets take their own presses; a macOS
					// Ctrl+click opens the owner's context menu instead
					continue
				}
				if b := v.copyButtonAt(e.Position); b >= 0 { // a code block's copy button
					v.copyPress = b + 1
					continue
				}
				v.press = v.targetAt(e.Position)
				v.pressPos = e.Position
				if c := hovercard.Default(); c != nil && v.isMention(v.press) {
					c.Pressed(mentionKey{v, v.press}) // Slack's handleClick closes the card
				}
				v.pressPreview(e.Position)
				v.moved = false
				if v.External {
					v.dragging = true
					v.tapCount = 1
					continue
				}
				off := v.offsetAt(e.Position)
				if e.Time-v.lastTap < 400*time.Millisecond {
					v.tapCount++
				} else {
					v.tapCount = 1
				}
				v.lastTap = e.Time
				switch {
				case v.tapCount == 2:
					v.selectWord(off)
				case v.tapCount >= 3:
					v.selectLine(off)
				case e.Modifiers.Contain(key.ModShift) && v.HasSelection():
					v.selB = off
				default:
					v.selA, v.selB = off, off
				}
				v.dragging = true
				gtx.Execute(key.FocusCmd{Tag: v})
			case pointer.Drag:
				if !v.dragging {
					continue
				}
				if d := e.Position.Sub(v.pressPos); d.X*d.X+d.Y*d.Y > 9 {
					v.moved = true
				}
				if v.moved && v.tapCount == 1 && !v.External {
					v.selB = v.offsetAt(e.Position)
				}
			case pointer.Release:
				if b := v.copyPress; b != 0 {
					v.copyPress = 0
					if v.copyButtonAt(e.Position)+1 == b {
						v.copyBlock(gtx, b-1)
					}
					continue
				}
				wasDrag := v.dragging
				v.dragging = false
				if !wasDrag {
					continue
				}
				if !v.moved && v.tapCount == 1 {
					if !v.External {
						v.selA, v.selB = 0, 0
					}
					if t := v.targetAt(e.Position); t >= 0 && t == v.press && h != nil {
						anchor := image.Pt(int(e.Position.X), int(e.Position.Y)).Add(v.Origin)
						if !v.clickLink(gtx, t, anchor, e, h) {
							v.activate(t, anchor, h)
						}
					}
				}
				v.press = -1
			}
		}
	}
}

// mentionKey identifies one @mention of a View to the hover card.
type mentionKey struct {
	v *View
	t int16
}

func (v *View) isMention(t int16) bool {
	return t >= 0 && int(t) < len(v.d.targets) && v.d.targets[t].kind == tUser && v.d.targets[t].id != ""
}

// hoverCard reports the @mention under the pointer to the profile hover
// card: Slack's MemberLink is a ProfileHoverTrigger, anchored at the
// mention's bounds (all its line boxes when it wraps).
func (v *View) hoverCard(st *Style, h Handler) {
	c := hovercard.Default()
	if c == nil {
		return
	}
	if v.opts.HoverCard {
		return // the hover card's own content triggers nothing
	}
	t := v.hover
	on := v.hovering && v.isMention(t)
	defer func() {
		if !on {
			v.reportPreview(c, st, h)
		}
	}()
	k := mentionKey{v, t}
	if v.hovOn && (!on || k != v.hovMen) {
		c.Left(v.hovMen)
	}
	v.hovOn, v.hovMen = on, k
	if !on {
		return
	}
	var r image.Rectangle
	for i := range v.d.frags {
		if f := &v.d.frags[i]; f.target == t {
			r = r.Union(fragRect(f))
		}
	}
	c.Report(k, v.d.targets[t].id, r, v.ptr)
}

// inSlot reports whether p is on an element run drawn by Options.Elements.
func (v *View) inSlot(p f32.Point) bool {
	pt := image.Pt(int(p.X), int(p.Y))
	for i := range v.d.slots {
		if pt.In(v.d.slots[i].r) {
			return true
		}
	}
	return false
}

// targetText is the text a link target draws (its frags' plain text).
func (v *View) targetText(t int16) string {
	var b strings.Builder
	for i := range v.d.frags {
		if f := &v.d.frags[i]; f.target == t && f.p1 > f.p0 && int(f.p1) <= len(v.d.plain) {
			b.WriteString(string(v.d.plain[f.p0:f.p1]))
		}
	}
	return b.String()
}

// clickLink sends a click on a link target to plugins that listen for
// clicks on the document (slackc.ClickLink), at the element Slack's DOM
// has for it. It reports whether it did: the link then activates when the
// click isn't canceled, possibly later (a replayed click), and never
// twice for one click.
func (v *View) clickLink(gtx layout.Context, t int16, anchor image.Point, e pointer.Event, h Handler) bool {
	if !slackc.WantsGlobal("click") {
		return false
	}
	tg := v.d.targets[t]
	text := v.targetText(t)
	var l slackc.NativeLink
	switch tg.kind {
	case tLink:
		if tg.url == "" {
			return false
		}
		l.Attrs = map[string]string{"class": "c-link", "href": tg.url, "target": "_blank", "rel": "noopener noreferrer"}
		if tg.slug {
			l.Attrs["class"] = "c-link p-rich_text_slug"
		}
	case tUser:
		l.Attrs = map[string]string{"class": "c-link c-member_slug c-member_slug--link", "data-member-id": tg.id, "data-stringify-type": "mention",
			"href": "/team/" + tg.id}
	case tChannel:
		l.Attrs = map[string]string{"class": "c-link c-inline_channel_entity", "data-channel-id": tg.id, "data-stringify-type": "mention",
			"href": "/archives/" + tg.id}
	case tUsergroup:
		l.Tag = "span"
		l.Attrs = map[string]string{"class": "c-mrkdwn__subteam c-mrkdwn__subteam--link", "data-usergroup-id": tg.id}
	default:
		return false
	}
	var r image.Rectangle
	for i := range v.d.frags {
		if f := &v.d.frags[i]; f.target == t {
			r = r.Union(fragRect(f))
		}
	}
	l.Rect = r.Add(v.Origin)
	l.Text = text
	l.Key = fmt.Sprintf("%p/%d/%s%s", v, t, tg.id, tg.url)
	// the click count Slack's event carries (detail)
	if t == v.clickT && e.Time-v.clickAt < 500*time.Millisecond {
		v.clickN++
	} else {
		v.clickN = 1
	}
	v.clickT, v.clickAt = t, e.Time
	return slackc.ClickLink(l, v.clickN, e.Modifiers, gtx.Metric.PxPerDp, func() { v.activateTarget(&tg, text, anchor, h) })
}

func (v *View) activate(t int16, anchor image.Point, h Handler) {
	v.activateTarget(&v.d.targets[t], v.targetText(t), anchor, h)
}

// activateTarget is a click on tg (text: what it draws).
func (v *View) activateTarget(tg *target, text string, anchor image.Point, h Handler) {
	switch tg.kind {
	case tLink, tCard:
		if tg.url == "" {
			break
		}
		if lh, ok := h.(LinkTextHandler); ok && tg.kind == tLink {
			lh.OpenLink(tg.url, text)
			break
		}
		h.OpenURL(tg.url)
	case tUser:
		h.OpenUser(tg.id, anchor)
	case tChannel:
		h.OpenChannel(tg.id)
	case tUsergroup:
		h.OpenUsergroup(tg.id, anchor)
	case tImage:
		if tg.img != nil {
			h.OpenImage(*tg.img)
		}
	case tFile:
		if tg.file != nil {
			h.OpenFile(tg.file)
		}
	case tButton:
		if tg.btn != nil {
			h.Button(*tg.btn)
		}
	case tRemove:
		if rm, ok := h.(AttachmentRemover); ok && tg.id != "" {
			rm.RemoveAttachment(tg.id)
		}
	case tBot:
		if bo, ok := h.(BotOpener); ok && tg.id != "" {
			bo.OpenBot(tg.id, anchor)
		}
	case tHuddleJoin:
		if hj, ok := h.(HuddleJoiner); ok && tg.id != "" {
			hj.JoinHuddle(tg.id)
		}
	case tCollapse:
		if tg.img != nil && v.collapsed != nil {
			v.collapsed[tg.img] = !v.collapsed[tg.img]
			v.dirty = true
			v.redraw = true
		}
	case tExpand:
		if tg.id != "" {
			setKitExpanded(tg.id, !kitIsExpanded(tg.id))
			v.dirty = true
			v.redraw = true
		} else if tg.att != nil && v.expanded != nil {
			v.expanded[tg.att] = !v.expanded[tg.att]
			v.dirty = true
			v.redraw = true
		}
	}
}

// FirstBaseline is the first line's baseline from the view's top (px),
// as a browser aligns inline content (firstBaseline); -1 before layout or
// when the first line holds nothing.
func (v *View) FirstBaseline() int { return v.d.firstBaseline() }

// ElementSlots are the boxes Options.Elements drew its element runs in
// (actions blocks, accessories), view-local, as of the last layout.
func (v *View) ElementSlots() []image.Rectangle {
	out := make([]image.Rectangle, len(v.d.slots))
	for i := range v.d.slots {
		out[i] = v.d.slots[i].r
	}
	return out
}

// Baselines returns the text baseline of each laid-out line, in px from
// the view's top (tests: row geometry against Slack's line boxes).
func (v *View) Baselines() []int {
	var out []int
	for _, ln := range v.d.lines {
		for i := ln.f0; i < ln.f1; i++ {
			if f := &v.d.frags[i]; f.kind == fText {
				out = append(out, f.y)
				break
			}
		}
	}
	return out
}

// paintGlyphs paints text frag f in col (emboldened like the static
// content), at its place.
func (v *View) paintGlyphs(ops *op.Ops, st *Style, f *frag, col color.NRGBA) {
	// Browsers render Lato visibly heavier than Gio's linear
	// coverage. Dark text gets the missing ink (st.Embolden px,
	// ~0.45 at 1x and 2x) from an emboldened outline: one fill,
	// no stroke tessellation (theme.EmboldenedShape).
	var emb clip.PathSpec
	embOK := false
	embW := st.Embolden
	if embW == 0 {
		embW = st.lightEmbolden(col, v.laidPx)
	}
	if embW > 0 {
		emb, embOK = st.emboldenedOf(f.sh, embW)
	}
	path, bm := st.pathOf(f.sh, !embOK)
	if embOK {
		path = emb
	}
	// Whole-pixel pen positions: Gio's path cache snaps fractional
	// offsets, which makes word spacing uneven.
	t := op.Offset(image.Pt(int(f.x+0.5), f.y)).Push(ops)
	cs := clip.Outline{Path: path}.Op().Push(ops)
	paint.ColorOp{Color: st.textColor(col, v.laidPx)}.Add(ops)
	paint.PaintOp{}.Add(ops)
	cs.Pop()
	if embW > 0 && !embOK { // fallback-font run
		// Browsers render Lato visibly heavier than Gio's linear
		// coverage; a hairline stroke of the outline matches it.
		// Width is in pixels, not dp: measured against Chrome, the
		// missing ink is ~0.45px at both 1x and 2x (thinning comes
		// from coverage at the pixel level, not from glyph size).
		paint.FillShape(ops, col, clip.Stroke{Path: path, Width: embW}.Op())
	}
	if f.sh.color {
		bm.Add(ops)
	}
	t.Pop()
}
