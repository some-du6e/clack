package slackc

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Replaced elements and form controls.

func accent() color.NRGBA {
	if th := env().Theme; th != nil {
		return th.C.Link
	}
	return color.NRGBA{R: 0x12, G: 0x64, B: 0xa3, A: 0xff}
}

func (p *painter) replaced(b *box, x, y float32, visible bool) {
	cx, cy := x+b.bd[3]+b.pad[3], y+b.bd[0]+b.pad[0]
	cw, ch := b.w-b.hpad(), b.h-b.vpad()
	r := rectF{cx, cy, cx + cw, cy + ch}
	switch b.repl {
	case rImg:
		p.img(b, r)
	case rSVG:
		p.svg(b, r)
	case rInput, rTextarea:
		p.input(b, r, visible)
	case rCheck, rRadio:
		p.check(b, r)
	case rRange:
		p.rangeCtl(b, r)
	case rProgress:
		p.progress(b, r)
	case rSelect:
		p.selectCtl(b, r)
	}
	if b.focusable() && p.s != nil && b.repl != rInput && b.repl != rTextarea {
		p.focusArea(b, rectF{x, y, x + b.w, y + b.h})
	}
}

func (p *painter) img(b *box, r rectF) {
	src, _ := b.n.AttrString("src")
	im := env().Images
	if im == nil || src == "" || r.empty() {
		return
	}
	img, ok := im.Get(src, 0)
	if !ok {
		if im.Failed(src, 0) {
			if alt, _ := b.n.AttrString("alt"); alt != "" {
				st := &b.st
				sh := word(p.v.face(st), alt)
				_, _, fa, _ := p.v.metrics(st)
				p.drawWord(sh, r.x0, r.y0+fa, p.col(st.Color), st)
			}
		} else {
			p.v.pendingImg = true
		}
		return
	}
	sz := img.Size()
	if sz.X <= 0 || sz.Y <= 0 {
		return
	}
	w, h := r.x1-r.x0, r.y1-r.y0
	iw, ih := float32(sz.X), float32(sz.Y)
	sx, sy := w/iw, h/ih
	switch b.st.ObjectFit {
	case css.FitContain:
		s := min(sx, sy)
		sx, sy = s, s
	case css.FitCover:
		s := max(sx, sy)
		sx, sy = s, s
	case css.FitNone:
		sx, sy = p.v.dp, p.v.dp
	case css.FitScaleDown:
		s := min(min(sx, sy), p.v.dp)
		sx, sy = s, s
	}
	dw, dh := iw*sx, ih*sy
	ox, oy := r.x0+(w-dw)/2, r.y0+(h-dh)/2
	rad := insetRad(p.v.radii(b, b.w, b.h), b.bd[0]+b.pad[0], b.bd[1]+b.pad[1], b.bd[2]+b.pad[2], b.bd[3]+b.pad[3])
	var cs clip.Stack
	if hasRadius(rad) {
		cs = clip.Outline{Path: rrectPath(p.ops, r, rad, false)}.Op().Push(p.ops)
	} else {
		cs = clip.Rect(irectSnap(r)).Push(p.ops)
	}
	t := op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(sx, sy)).Offset(f32.Pt(ox, oy))).Push(p.ops)
	img.Add(p.ops)
	paint.PaintOp{}.Add(p.ops)
	t.Pop()
	cs.Pop()
}

func (p *painter) svg(b *box, r rectF) {
	if b.svg == nil {
		b.svg = compileSVG(b.n)
	}
	st := &b.st
	root := svgPaint{Color: st.Color, Fill: st.Fill, FillNone: st.FillNone, Stroke: st.Stroke, StrokeNone: st.StrokeNone, StrokeWidth: st.StrokeWidth, Opacity: 1}
	var mc func(color.NRGBA) color.NRGBA
	if len(p.filt) > 0 {
		// a func value made once per View: a method value on p would make
		// every painter escape
		p.v.cmap.filt = p.filt
		mc = p.v.cmapFn
	}
	var sty func(*Node) *svgCSS
	if p.v.svgCSS != nil {
		sty = p.v.svgStyFn
	}
	t := op.Offset(image.Pt(int(math.Round(float64(r.x0))), int(math.Round(float64(r.y0))))).Push(p.ops)
	b.svg.paint(p.ops, f32.Pt(r.x1-r.x0, r.y1-r.y0), root, sty, mc)
	t.Pop()
}

func (v *View) svgSty(n *Node) *svgCSS { return v.svgCSS[n] }

// svgStyles cascades CSS rules for an svg subtree's nodes.
func (v *View) svgStyles(b *box, e *css.Env) {
	if Styles.Empty() {
		return
	}
	var walk func(n *Node, parent css.Element, pst *css.Style)
	walk = func(n *Node, parent css.Element, pst *css.Style) {
		for _, k := range n.C {
			if k.IsText() {
				continue
			}
			el := &nodeEl{n: k, parent: parent}
			c := Styles.Cascade(el, css.PseudoNone, k.Style, *e)
			st := css.Compute(&c, pst, *e)
			sc := &svgCSS{}
			set := false
			if c.Has("fill") {
				set = true
				sc.FillSet = true
				sc.Fill, sc.FillNone = st.Fill, st.FillNone
				if fv, _ := c.Get("fill"); strings.EqualFold(strings.TrimSpace(fv), "currentcolor") {
					sc.FillCurrent = true
				}
			}
			if c.Has("stroke") {
				set = true
				sc.StrokeSet = true
				sc.Stroke, sc.StrokeNone = st.Stroke, st.StrokeNone
				if sv, _ := c.Get("stroke"); strings.EqualFold(strings.TrimSpace(sv), "currentcolor") {
					sc.StrokeCurrent = true
				}
			}
			if c.Has("stroke-width") {
				set = true
				sc.StrokeWidthSet, sc.StrokeWidth = true, st.StrokeWidth
			}
			if c.Has("opacity") {
				set = true
				sc.OpacitySet, sc.Opacity = true, st.Opacity
			}
			if c.Has("color") {
				set = true
				sc.ColorSet, sc.Color = true, st.Color
			}
			if st.Display == css.DisplayNone && c.Has("display") || st.Visibility != css.Visible && c.Has("visibility") {
				set = true
				sc.Hidden = true
			}
			if set {
				if v.svgCSS == nil {
					v.svgCSS = map[*Node]*svgCSS{}
				}
				v.svgCSS[k] = sc
			}
			walk(k, el, &st)
		}
	}
	walk(b.n, b, &b.st)
}

// nodeEl adapts a raw svg node for selector matching.
type nodeEl struct {
	n      *Node
	parent css.Element
}

func (e *nodeEl) Tag() string { return e.n.T }
func (e *nodeEl) ID() string  { return e.n.ID }
func (e *nodeEl) HasClass(c string) bool {
	for _, x := range e.n.Class {
		if x == c {
			return true
		}
	}
	return false
}
func (e *nodeEl) Classes() []string               { return e.n.Class }
func (e *nodeEl) Attr(name string) (string, bool) { return e.n.AttrString(name) }
func (e *nodeEl) Parent() css.Element             { return e.parent }
func (e *nodeEl) Prev() css.Element               { return nil }
func (e *nodeEl) Index() (int, int)               { return e.n.index, e.n.nsib }
func (e *nodeEl) TypeIndex() (int, int)           { return e.n.tindex, e.n.ntsib }
func (e *nodeEl) State() css.State                { return 0 }

// --- text inputs ---

func (v *View) editor(b *box) *widget.Editor {
	ns := v.state(b)
	if ns.ed == nil {
		ns.ed = &widget.Editor{}
		if val, ok := nodeValue(b.n, "value"); ok {
			ns.ed.SetText(val)
		} else if val, ok := nodeValue(b.n, "defaultValue"); ok {
			ns.ed.SetText(val)
		}
		ns.lastText = ns.ed.Text()
		ns.edNode = b.n
	}
	return ns.ed
}

func nodeValue(n *Node, prop string) (string, bool) {
	if n == nil {
		return "", false
	}
	switch x := n.P[prop].(type) {
	case string:
		return x, true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case nil:
		return "", false
	case bool:
		return "", false
	}
	return "", false
}

// syncControlled applies React controlled-input semantics: when a new
// tree's value differs from what the user last typed (and isn't a
// pending echo of an earlier keystroke), adopt it.
func (v *View) syncControlled(b *box, ed *widget.Editor) {
	ns := v.state(b)
	if ns.edNode == b.n {
		return
	}
	ns.edNode = b.n
	val, ok := nodeValue(b.n, "value")
	if !ok {
		return
	}
	cur := ed.Text()
	for i, s := range ns.sent {
		if s == val {
			if i == len(ns.sent)-1 {
				ns.sent = ns.sent[:0]
				return
			}
			// an echo of an older keystroke: newer ones are in flight
			ns.sent = append(ns.sent[:0], ns.sent[i+1:]...)
			return
		}
	}
	ns.sent = ns.sent[:0]
	if val != cur {
		ed.SetText(val)
		ns.lastText = val
	}
}

func (p *painter) input(b *box, r rectF, visible bool) {
	v := p.v
	s := p.s
	if s == nil {
		return
	}
	st := &b.st
	ns := v.state(b)
	ed := v.editor(b)
	v.syncControlled(b, ed)
	ed.SingleLine = b.repl == rInput
	ed.Submit = b.repl == rInput
	ed.ReadOnly = truthy(b.n.P["readOnly"]) || b.disabled()
	ed.MaxLen = 0
	if ml, ok := attrNum(b.n, "maxLength"); ok && ml > 0 {
		ed.MaxLen = int(ml)
	}
	ed.Mask = 0
	if t, _ := b.n.AttrString("type"); strings.EqualFold(t, "password") {
		ed.Mask = '•'
	}
	lh := v.lineHeight(st)
	ed.LineHeight = unit.Sp(lh / v.sp)
	ed.WrapPolicy = 0
	gtx := s.gtx
	if b.disabled() {
		gtx = gtx.Disabled()
	}
	if truthy(b.n.P["autoFocus"]) && !ns.autoFocused {
		ns.autoFocused = true
		gtx.Execute(key.FocusCmd{Tag: ed})
	} else if modalFocus(b, ns) {
		gtx.Execute(key.FocusCmd{Tag: ed})
	}
	// keys the editor doesn't report: Escape, and arrows in one-line inputs
	if ns.filters == nil {
		if b.repl == rInput {
			ns.filters = []event.Filter{key.Filter{Focus: ed, Name: key.NameEscape}, key.Filter{Focus: ed, Name: key.NameUpArrow}, key.Filter{Focus: ed, Name: key.NameDownArrow}}
		} else {
			ns.filters = []event.Filter{key.Filter{Focus: ed, Name: key.NameEscape}}
		}
	}
	for {
		e, ok := gtx.Event(ns.filters...)
		if !ok {
			break
		}
		if ke, ok := e.(key.Event); ok {
			typ := "keydown"
			if ke.State == key.Release {
				typ = "keyup"
			}
			v.dispatchKey(b, typ, ke)
		}
	}
	for {
		e, ok := ed.Update(gtx)
		if !ok {
			break
		}
		switch e.(type) {
		case widget.ChangeEvent:
			txt := ed.Text()
			if txt == ns.lastText {
				continue
			}
			if ins := typedText(ns.lastText, txt); ins != "" {
				for _, r := range ins {
					v.dispatchChar(b, string(r))
				}
			} else if len(txt) < len(ns.lastText) {
				v.dispatchKey(b, "keydown", key.Event{Name: key.NameDeleteBackward})
			}
			ns.lastText = txt
			if len(ns.sent) < 64 {
				ns.sent = append(ns.sent, txt)
			}
			v.dispatchValue(b, "input")
		case widget.SubmitEvent:
			v.dispatchKey(b, "keydown", key.Event{Name: key.NameReturn})
			v.dispatchKey(b, "keyup", key.Event{Name: key.NameReturn, State: key.Release})
			v.dispatchValue(b, "change")
			ns.focusVal = ed.Text()
			if f := formOf(b); f != nil {
				v.dispatchSimple(f, "submit", nil)
			}
		}
	}
	v.focusEvents(s, b)
	if ed.Len() == 0 {
		v.setState(b, css.StatePlaceholderShown, true)
	} else {
		v.setState(b, css.StatePlaceholderShown, false)
	}
	if !visible {
		return
	}
	cw, ch := r.x1-r.x0, r.y1-r.y0
	oy := float32(0)
	if b.repl == rInput && ch > lh {
		oy = (ch - lh) / 2
	}
	// placeholder
	if ed.Len() == 0 {
		if ph, ok := b.n.AttrString("placeholder"); ok && ph != "" {
			pst := b.ph
			if pst == nil {
				pst = st
			}
			col := pst.Color
			if b.ph == nil {
				col.A = uint8(float32(col.A) * 0.55)
			}
			fk := v.face(pst)
			sh := word(fk, ph)
			a, _, _, _ := v.metrics(pst)
			cs := clip.Rect(irectSnap(r)).Push(p.ops)
			p.drawWord(sh, r.x0, r.y0+oy+a, p.col(col), pst)
			cs.Pop()
		}
	}
	g := gtx
	g.Constraints = layout.Exact(image.Pt(ceilI(cw), ceilI(ch-oy)))
	if b.repl == rInput {
		g.Constraints = layout.Exact(image.Pt(ceilI(cw), ceilI(min(lh, ch))))
	}
	t := op.Offset(image.Pt(int(math.Round(float64(r.x0))), int(math.Round(float64(r.y0+oy))))).Push(p.ops)
	cs := clip.Rect(image.Rectangle{Max: g.Constraints.Max}).Push(p.ops)
	selCol := accent()
	selCol.A = 0x50
	sz := unit.Sp(st.FontSize)
	d := ed.Layout(g, shaper(), theme.EditorFont(fontFor(st)), sz, theme.Material(g, p.col(st.Color)), theme.Material(g, selCol))
	theme.EditorEmoji(g, shaper(), ed, sz, d) // emoji as Slack's images (theme/editoremoji.go)
	cs.Pop()
	t.Pop()
}

// typedText returns the few characters typed between a and b (a pure
// insertion of up to 8 runes; several can land in one frame).
func typedText(a, b string) string {
	if len(b) <= len(a) {
		return ""
	}
	i := 0
	for i < len(a) && a[i] == b[i] {
		i++
	}
	j := 0
	for j < len(a)-i && a[len(a)-1-j] == b[len(b)-1-j] {
		j++
	}
	ins := b[i : len(b)-j]
	if n := utf8.RuneCountInString(ins); n >= 1 && n <= 8 {
		return ins
	}
	return ""
}

// modalFocus reports a JS focus (el.focus(), the dialog focusing steps of
// showModal()) of b inside an open modal dialog that key focus hasn't
// followed yet. JS focus elsewhere stays JS-side, so a plugin can't take
// the composer's keys; in a modal dialog the page is inert, as in browsers.
func modalFocus(b *box, ns *nodeState) bool {
	if !b.n.Focus {
		ns.jsFocused = false
		return false
	}
	if ns.jsFocused {
		return false
	}
	for x := b; x != nil; x = x.parent {
		if x.topLayer {
			ns.jsFocused = true
			return true
		}
	}
	return false
}

// focusArea registers a non-editor focusable for key focus and Tab.
func (p *painter) focusArea(b *box, r rectF) {
	v, s := p.v, p.s
	ns := v.state(b)
	ar := clip.Rect(irect(r)).Push(p.ops)
	pp := pointer.PassOp{}.Push(p.ops)
	event.Op(p.ops, &ns.tag)
	pp.Pop()
	ar.Pop()
	if truthy(b.n.P["autoFocus"]) && !ns.autoFocused {
		ns.autoFocused = true
		s.gtx.Execute(key.FocusCmd{Tag: &ns.tag})
	} else if modalFocus(b, ns) {
		s.gtx.Execute(key.FocusCmd{Tag: &ns.tag})
	}
	v.keyEvents(s, b)
	v.focusEvents(s, b)
}

// --- checkbox / radio / range / progress / select ---

func (v *View) checked(b *box) bool {
	ns := v.state(b)
	if ns.chkNode != b.n {
		ns.chkNode = b.n
		if _, ok := b.n.P["checked"]; ok {
			ns.checkedSet = false
		} else if !ns.checkedSet {
			ns.checked = truthy(b.n.P["defaultChecked"])
			ns.checkedSet = true
		}
	}
	if ns.checkedSet {
		return ns.checked
	}
	return truthy(b.n.P["checked"])
}

func (v *View) toggle(s *scope, b *box) {
	cur := v.checked(b)
	nv := !cur
	if b.repl == rRadio {
		nv = true
	}
	ns := v.state(b)
	ns.checked, ns.checkedSet = nv, true
	v.dispatchValue(b, "input")
	v.dispatchValue(b, "change")
	v.invalidate(s)
}

func (p *painter) check(b *box, r rectF) {
	v := p.v
	dp := v.dp
	on := v.checked(b)
	th := env().Theme
	border := color.NRGBA{A: 0x80}
	bg := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	if th != nil {
		border = th.C.Border
		bg = th.C.Bg
		border.A = max(border.A, 0x90)
	}
	if b.disabled() {
		border.A /= 2
	}
	ac := accent()
	if b.repl == rRadio {
		e := irectSnap(r)
		if on {
			paint.FillShape(p.ops, p.col(ac), clip.Ellipse(e).Op(p.ops))
			d := (r.x1 - r.x0) * 0.36
			c := image.Rect(int((r.x0+r.x1)/2-d/2+0.5), int((r.y0+r.y1)/2-d/2+0.5), int((r.x0+r.x1)/2+d/2+0.5), int((r.y0+r.y1)/2+d/2+0.5))
			paint.FillShape(p.ops, p.col(bg), clip.Ellipse(c).Op(p.ops))
		} else {
			paint.FillShape(p.ops, p.col(bg), clip.Ellipse(e).Op(p.ops))
			paint.FillShape(p.ops, p.col(border), clip.Stroke{Path: clip.Ellipse(e.Inset(int(dp / 2))).Path(p.ops), Width: dp}.Op())
		}
		return
	}
	rad := [4]float32{3 * dp, 3 * dp, 3 * dp, 3 * dp}
	if on {
		p.fillRR(r, rad, ac)
		var pa clip.Path
		pa.Begin(p.ops)
		w, h := r.x1-r.x0, r.y1-r.y0
		pa.MoveTo(f32.Pt(r.x0+w*0.22, r.y0+h*0.52))
		pa.LineTo(f32.Pt(r.x0+w*0.42, r.y0+h*0.72))
		pa.LineTo(f32.Pt(r.x0+w*0.78, r.y0+h*0.30))
		paint.FillShape(p.ops, p.col(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}), clip.Stroke{Path: pa.End(), Width: 1.8 * dp}.Op())
		return
	}
	p.fillRR(r, rad, bg)
	paint.FillShape(p.ops, p.col(border), clip.Outline{Path: ringPath(p.ops, r, rad, rectF{r.x0 + dp, r.y0 + dp, r.x1 - dp, r.y1 - dp}, shrinkRad(rad, dp))}.Op())
}

func rangeAttrs(n *Node) (lo, hi, step float64) {
	lo, hi, step = 0, 100, 1
	if f, ok := attrNum(n, "min"); ok {
		lo = float64(f)
	}
	if f, ok := attrNum(n, "max"); ok {
		hi = float64(f)
	}
	if f, ok := attrNum(n, "step"); ok && f > 0 {
		step = float64(f)
	}
	if hi < lo {
		hi = lo
	}
	return
}

func (v *View) rangeValue(b *box) float64 {
	lo, hi, _ := rangeAttrs(b.n)
	ns := v.state(b)
	if ns.rangeDrag {
		return ns.rangeV
	}
	if f, ok := attrNum(b.n, "value"); ok {
		return math.Max(lo, math.Min(hi, float64(f)))
	}
	if f, ok := attrNum(b.n, "defaultValue"); ok && !ns.valueSet {
		return float64(f)
	}
	if ns.valueSet {
		return ns.rangeV
	}
	return (lo + hi) / 2
}

func (v *View) rangeSet(s *scope, b *box, x float32, commit bool) {
	lo, hi, step := rangeAttrs(b.n)
	ns := v.state(b)
	cx := b.ax + b.bd[3] + b.pad[3]
	cw := b.w - b.hpad()
	t := 0.0
	if cw > 0 {
		t = math.Max(0, math.Min(1, float64((x-cx)/cw)))
	}
	val := lo + t*(hi-lo)
	val = lo + math.Round((val-lo)/step)*step
	val = math.Max(lo, math.Min(hi, val))
	changed := val != v.rangeValue(b)
	ns.rangeV, ns.valueSet = val, true
	ns.rangeDrag = !commit
	if changed {
		v.dispatchValue(b, "input")
	}
	if commit {
		v.dispatchValue(b, "change")
	}
	v.invalidate(s)
}

func (p *painter) rangeCtl(b *box, r rectF) {
	v := p.v
	dp := v.dp
	lo, hi, _ := rangeAttrs(b.n)
	val := v.rangeValue(b)
	t := float32(0)
	if hi > lo {
		t = float32((val - lo) / (hi - lo))
	}
	cy := (r.y0 + r.y1) / 2
	th := env().Theme
	track := color.NRGBA{A: 0x30}
	if th != nil {
		track = th.C.Border
	}
	tr := rectF{r.x0, cy - 2*dp, r.x1, cy + 2*dp}
	rad := [4]float32{2 * dp, 2 * dp, 2 * dp, 2 * dp}
	p.fillRR(tr, rad, track)
	x := r.x0 + (r.x1-r.x0)*t
	p.fillRR(rectF{r.x0, tr.y0, x, tr.y1}, rad, accent())
	k := 8 * dp
	e := image.Rect(int(x-k), int(cy-k), int(x+k), int(cy+k))
	paint.FillShape(p.ops, p.col(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}), clip.Ellipse(e).Op(p.ops))
	paint.FillShape(p.ops, p.col(accent()), clip.Stroke{Path: clip.Ellipse(e.Inset(int(dp))).Path(p.ops), Width: 2 * dp}.Op())
}

func (p *painter) progress(b *box, r rectF) {
	val, hasVal := attrNum(b.n, "value")
	mx, ok := attrNum(b.n, "max")
	if !ok || mx <= 0 {
		mx = 1
	}
	dp := p.v.dp
	rad := [4]float32{4 * dp, 4 * dp, 4 * dp, 4 * dp}
	th := env().Theme
	track := color.NRGBA{A: 0x22}
	if th != nil {
		track = th.C.Border
	}
	p.fillRR(r, rad, track)
	t := float32(0.3)
	if hasVal {
		t = clampF(val/mx, 0, 1)
	}
	p.fillRR(rectF{r.x0, r.y0, r.x0 + (r.x1-r.x0)*t, r.y1}, rad, accent())
}

type selOpt struct {
	value, label string
	disabled     bool
	selected     bool
}

func textOf(n *Node) string {
	if n.IsText() {
		return n.V
	}
	var b strings.Builder
	for _, k := range n.C {
		b.WriteString(textOf(k))
	}
	return b.String()
}

func selectOptions(n *Node) []selOpt {
	var out []selOpt
	var walk func(list []*Node)
	walk = func(list []*Node) {
		for _, k := range list {
			switch k.T {
			case "option":
				label := strings.TrimSpace(textOf(k))
				if l, ok := k.AttrString("label"); ok && l != "" {
					label = l
				}
				val, ok := k.AttrString("value")
				if !ok {
					val = label
				}
				out = append(out, selOpt{value: val, label: label, disabled: truthy(k.P["disabled"]), selected: truthy(k.P["selected"])})
			case "optgroup":
				walk(k.C)
			}
		}
	}
	walk(n.C)
	return out
}

func (v *View) selectValue(b *box) string {
	ns := v.state(b)
	if val, ok := nodeValue(b.n, "value"); ok && !(ns.valueSet && ns.chkNode == b.n) {
		return val
	}
	if ns.valueSet {
		return ns.value
	}
	if val, ok := nodeValue(b.n, "defaultValue"); ok {
		return val
	}
	opts := selectOptions(b.n)
	for _, o := range opts {
		if o.selected {
			return o.value
		}
	}
	if len(opts) > 0 {
		return opts[0].value
	}
	return ""
}

func (p *painter) selectCtl(b *box, r rectF) {
	v := p.v
	st := &b.st
	val := v.selectValue(b)
	label := val
	for _, o := range selectOptions(b.n) {
		if o.value == val {
			label = o.label
			break
		}
	}
	a, _, _, _ := v.metrics(st)
	cs := clip.Rect(irectSnap(r)).Push(p.ops)
	p.drawWord(word(v.face(st), label), r.x0, r.y0+(r.y1-r.y0-v.lineHeight(st))/2+a, p.col(st.Color), st)
	cs.Pop()
	// caret
	dp := v.dp
	cx, cy := r.x1-8*dp, (r.y0+r.y1)/2
	var pa clip.Path
	pa.Begin(p.ops)
	pa.MoveTo(f32.Pt(cx-4*dp, cy-2*dp))
	pa.LineTo(f32.Pt(cx+4*dp, cy-2*dp))
	pa.LineTo(f32.Pt(cx, cy+3*dp))
	pa.Close()
	paint.FillShape(p.ops, p.col(st.Color), clip.Outline{Path: pa.End()}.Op())
}

// dropState is an open native <select> dropdown.
type dropState struct {
	open  bool
	k     int64
	r     rectF
	rowH  float32
	opts  []selOpt
	hover int
}

func (s *scope) toggleDropdown(b *box) {
	d := &s.dropdown
	if d.open && d.k == b.k() {
		d.open = false
		return
	}
	if b.disabled() {
		return
	}
	v := s.v
	d.open, d.k = true, b.k()
	d.opts = selectOptions(b.n)
	d.rowH = v.lineHeight(&b.st) + 8*v.dp
	w := max(b.w, 120*v.dp)
	d.r = rectF{b.ax, b.ay + b.h + 2*v.dp, b.ax + w, b.ay + b.h + 2*v.dp + d.rowH*float32(len(d.opts)) + 8*v.dp}
	d.hover = -1
	v.invalidate(s)
}

func (s *scope) dropdownMove(pos f32.Point) {
	d := &s.dropdown
	i := -1
	if d.r.contains(pos.X, pos.Y) {
		i = int((pos.Y - d.r.y0 - 4*s.v.dp) / d.rowH)
	}
	if i != d.hover {
		d.hover = i
		s.v.invalidate(s)
	}
}

// dropdownPress handles a press while the dropdown is open; it reports
// whether the press was consumed.
func (s *scope) dropdownPress(pos f32.Point) bool {
	d := &s.dropdown
	v := s.v
	b := v.byK[d.k]
	if !d.r.contains(pos.X, pos.Y) {
		d.open = false
		v.invalidate(s)
		// a press on the select itself just closes it
		return b != nil && rectF{b.ax, b.ay, b.ax + b.w, b.ay + b.h}.contains(pos.X, pos.Y)
	}
	i := int((pos.Y - d.r.y0 - 4*v.dp) / d.rowH)
	d.open = false
	if b != nil && i >= 0 && i < len(d.opts) && !d.opts[i].disabled {
		ns := v.state(b)
		ns.value, ns.valueSet = d.opts[i].value, true
		ns.chkNode = b.n
		v.dispatchValue(b, "input")
		v.dispatchValue(b, "change")
	}
	v.invalidate(s)
	return true
}

func (s *scope) drawDropdown(p *painter) {
	d := &s.dropdown
	if !d.open {
		return
	}
	v := s.v
	b := v.byK[d.k]
	if b == nil {
		d.open = false
		return
	}
	th := env().Theme
	bg, fg, hov := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, color.NRGBA{A: 0xff}, color.NRGBA{R: 0x12, G: 0x64, B: 0xa3, A: 0xff}
	if th != nil {
		bg, fg, hov = th.C.BgRaised, th.C.Fg, th.C.Link
	}
	m := op.Record(p.ops)
	dp := v.dp
	rad := [4]float32{6 * dp, 6 * dp, 6 * dp, 6 * dp}
	sh := color.NRGBA{A: 0x40}
	p.fillRaw(rectF{d.r.x0 - dp, d.r.y0, d.r.x1 + dp, d.r.y1 + 2*dp}, rad, sh)
	p.fillRR(d.r, rad, bg)
	cur := v.selectValue(b)
	st := &b.st
	a, _, _, _ := v.metrics(st)
	for i, o := range d.opts {
		y := d.r.y0 + 4*dp + float32(i)*d.rowH
		col := fg
		if i == d.hover && !o.disabled {
			p.fillRR(rectF{d.r.x0 + 4*dp, y, d.r.x1 - 4*dp, y + d.rowH}, [4]float32{4 * dp, 4 * dp, 4 * dp, 4 * dp}, hov)
			col = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
		}
		if o.disabled {
			col.A /= 2
		}
		label := o.label
		if o.value == cur {
			label = "✓ " + label
		}
		p.drawWord(word(v.face(st), label), d.r.x0+12*dp, y+4*dp+a, col, st)
	}
	ar := clip.Rect(irect(d.r)).Push(p.ops)
	event.Op(p.ops, &s.stag)
	pp := pointer.PassOp{}.Push(p.ops)
	event.Op(p.ops, &s.ptag)
	pp.Pop()
	ar.Pop()
	op.Defer(p.ops, m.Stop())
}

// targetValue is event.target.value for t.
func (v *View) targetValue(t *box) (string, bool) {
	switch t.repl {
	case rInput, rTextarea:
		if ns := t.ns; ns != nil && ns.ed != nil {
			return ns.ed.Text(), true
		}
		if val, ok := nodeValue(t.n, "value"); ok {
			return val, true
		}
		return "", true
	case rSelect:
		return v.selectValue(t), true
	case rRange:
		return strconv.FormatFloat(v.rangeValue(t), 'f', -1, 64), true
	case rCheck, rRadio:
		if val, ok := nodeValue(t.n, "value"); ok {
			return val, true
		}
		return "on", true
	}
	if t.n != nil {
		if val, ok := nodeValue(t.n, "value"); ok {
			return val, true
		}
	}
	return "", false
}
