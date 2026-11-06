package richview

import (
	"image"
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

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
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
	dirty   bool

	ops      op.Ops
	call     op.CallOp
	recorded bool
	pending  []int32 // imgs (>=0) or ^frag index of images not yet decoded

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
	hovering bool

	// Origin, if set by the owner, is added to click anchors so they are in
	// window coordinates.
	Origin image.Point
}

// New returns a View for doc.
func New(doc richtext.Doc, o Options) *View {
	v := &View{hover: -1, press: -1}
	v.Set(doc, o)
	return v
}

// Set replaces the document. Call it only when the message changed.
func (v *View) Set(doc richtext.Doc, o Options) {
	v.doc = doc
	v.opts = o
	v.dirty = true
	v.selA, v.selB = 0, 0
	v.hover, v.press = -1, -1
}

// SetOptions changes only the options (e.g. edited flag, pending state).
func (v *View) SetOptions(o Options) {
	if o != v.opts {
		v.opts = o
		v.dirty = true
	}
}

// Doc returns the current document.
func (v *View) Doc() richtext.Doc { return v.doc }

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
	if v.dirty || w != v.laidW || gtx.Metric.PxPerDp != v.laidPx || gtx.Metric.PxPerSp != v.laidSp || st.gen != v.laidGen {
		v.relayout(st, w, gtx.Metric.PxPerDp, gtx.Metric.PxPerSp)
	} else if len(v.pending) > 0 && v.anyReady(st) {
		v.recorded = false
	}
	if !v.recorded {
		v.record(st)
	}
	v.events(gtx, st, h)

	size := v.d.size
	if gtx.Constraints.Min.X > size.X {
		size.X = gtx.Constraints.Min.X
	}
	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
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
			if b.target == v.hover && b.hover.A > 0 {
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
	if v.hover >= 0 && int(v.hover) < len(v.d.targets) && v.d.targets[v.hover].underline {
		for i := range v.d.frags {
			f := &v.d.frags[i]
			if f.target == v.hover && f.kind == fText {
				underline(gtx.Ops, f, st)
			}
		}
	}
	if v.opts.Dim {
		opac.Pop()
	}

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
	for i := range v.d.frags {
		f := &v.d.frags[i]
		if f.target >= 0 {
			s := clip.Rect(fragRect(f)).Push(gtx.Ops)
			pointer.CursorPointer.Add(gtx.Ops)
			s.Pop()
		}
	}
	return layout.Dimensions{Size: size, Baseline: 0}
}

func fragRect(f *frag) image.Rectangle {
	return image.Rect(int(f.x), f.top, int(f.x+f.w+0.999), f.bot)
}

func (v *View) relayout(st *Style, w int, pxdp, pxsp float32) {
	v.d.reset()
	l := lay{st: st, d: &v.d, scale: pxdp, spx: pxsp, jumbo: v.doc.Jumbo, opts: v.opts}
	if l.scale <= 0 {
		l.scale = 1
	}
	if l.spx <= 0 {
		l.spx = l.scale
	}
	h := l.blocks(v.doc.Blocks, 0, 0, w, true)
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
	if maxX > w {
		maxX = w
	}
	v.d.size = image.Pt(maxX, h)
	v.laidW, v.laidPx, v.laidSp, v.laidGen = w, pxdp, pxsp, st.gen
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
	ops := &v.ops
	m := op.Record(ops)
	for i := range v.d.boxes {
		if b := &v.d.boxes[i]; b.layer == 0 {
			drawBox(ops, b)
		}
	}
	for i := range v.d.imgs {
		im := &v.d.imgs[i]
		if im.url == "" {
			fillRadii(ops, im.r, im.radii, st.Palette.Placeholder)
			continue
		}
		var img paint.ImageOp
		ok := false
		if st.Images != nil {
			img, ok = st.Images.Get(im.url, im.px)
		}
		if !ok {
			fillRadii(ops, im.r, im.radii, st.Palette.Placeholder)
			v.pending = append(v.pending, int32(i))
			continue
		}
		if im.fit {
			fillRadii(ops, im.r, im.radii, st.Palette.Placeholder)
		}
		drawImage(ops, img, im.r, im.radii, !im.fit)
	}
	for i := range v.d.frags {
		f := &v.d.frags[i]
		switch f.kind {
		case fImage:
			var img paint.ImageOp
			ok := false
			if st.Images != nil {
				img, ok = st.Images.Get(f.url, f.size)
			}
			if !ok {
				v.pending = append(v.pending, ^int32(i))
				continue
			}
			r := image.Rect(int(f.x+0.5), f.y, int(f.x+0.5)+f.size, f.y+f.size)
			drawImage(ops, img, r, [4]int{}, false)
		case fText:
			if f.sh == nil || len(f.sh.glyphs) == 0 {
				continue
			}
			path, bm := st.pathOf(f.sh)
			// Whole-pixel pen positions: Gio's path cache snaps fractional
			// offsets, which makes word spacing uneven.
			t := op.Offset(image.Pt(int(f.x+0.5), f.y)).Push(ops)
			cs := clip.Outline{Path: path}.Op().Push(ops)
			paint.ColorOp{Color: f.col}.Add(ops)
			paint.PaintOp{}.Add(ops)
			cs.Pop()
			if f.sh.color {
				bm.Add(ops)
			}
			t.Pop()
			if f.style&(richtext.Strike|richtext.Underline) != 0 {
				thick := max(1, f.size/14)
				if f.style&richtext.Strike != 0 {
					y := f.y - int(float32(f.size)*0.3+0.5)
					paint.FillShape(ops, f.col, clip.Rect(image.Rect(int(f.x), y, int(f.x+f.w+0.5), y+thick)).Op())
				}
				if f.style&richtext.Underline != 0 {
					underline(ops, f, st)
				}
			}
		}
	}
	for i := range v.d.boxes {
		if b := &v.d.boxes[i]; b.layer == 1 {
			drawBox(ops, b)
		}
	}
	v.call = m.Stop()
	v.recorded = true
}

func underline(ops *op.Ops, f *frag, st *Style) {
	thick := max(1, f.size/14)
	y := f.y + max(1, f.size/8)
	paint.FillShape(ops, f.col, clip.Rect(image.Rect(int(f.x), y, int(f.x+f.w+0.5), y+thick)).Op())
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
		r := image.Rect(int(x0), f.top, int(x1+0.999), f.bot)
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
			if !e.Focus && !v.dragging {
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
			case pointer.Leave:
				v.hovering = false
				if !v.dragging {
					v.hover = -1
				}
			case pointer.Cancel:
				v.dragging = false
				v.press = -1
			case pointer.Press:
				if e.Buttons != pointer.ButtonPrimary {
					continue
				}
				v.press = v.targetAt(e.Position)
				v.pressPos = e.Position
				v.moved = false
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
				if v.moved && v.tapCount == 1 {
					v.selB = v.offsetAt(e.Position)
				}
			case pointer.Release:
				wasDrag := v.dragging
				v.dragging = false
				if !wasDrag {
					continue
				}
				if !v.moved && v.tapCount == 1 {
					v.selA, v.selB = 0, 0
					if t := v.targetAt(e.Position); t >= 0 && t == v.press && h != nil {
						anchor := image.Pt(int(e.Position.X), int(e.Position.Y)).Add(v.Origin)
						v.activate(t, anchor, h)
					}
				}
				v.press = -1
			}
		}
	}
}

func (v *View) activate(t int16, anchor image.Point, h Handler) {
	tg := &v.d.targets[t]
	switch tg.kind {
	case tLink, tCard:
		if tg.url != "" {
			h.OpenURL(tg.url)
		}
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
	}
}
