package slackc

import (
	"image/color"
	"strings"

	"gioui.org/op"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
)

// The box tree mirrors a delivered host tree: one box per node, plus
// generated boxes (::before/::after, anonymous flex items). Trees are
// immutable, so a box tree is built once per delivered tree and keeps
// its computed styles, measurements and last layout until the tree, the
// style sheets or some node's state change.

type boxKind uint8

const (
	kElem   boxKind = iota // host element (div, span, img, svg, input…)
	kText                  // "#" text, or the text of a ::before/::after box
	kComp                  // "@" Slack component
	kPseudo                // ::before / ::after
	kAnon                  // anonymous block wrapping text runs in flex/grid containers
)

// replaced element kinds (their content isn't laid out from children).
type replKind uint8

const (
	rNone replKind = iota
	rImg
	rSVG
	rInput // text-like input
	rTextarea
	rCheck // checkbox
	rRadio
	rRange
	rSelect
	rProgress
	rBr
	rEmbed // iframe/video/canvas/object: empty box
)

type box struct {
	n      *Node // nil for anonymous and pseudo boxes
	parent *box
	kids   []*box // tree children (text and elements)
	lk     []*box // layout children: kids + pseudos, anonymous-wrapped for flex/grid
	kind   boxKind
	repl   replKind
	text   string // kText: the text (generated content for pseudo text)
	v      *View
	scope  *scope // scope the box was last laid out in
	ns     *nodeState

	// style
	st         css.Style
	set        propSet
	state      css.State
	key        uint64
	cursor     css.Cursor
	before     *box
	after      *box
	ph         *css.Style // ::placeholder (inputs)
	listMarker int8       // 0 none, 1 disc, 2 decimal, 3 circle, 4 square; -1 pending
	listIndex  int
	ekids      []css.Element
	ekidsOK    bool
	listens    uint32 // listened event groups (On on this node)
	subListens uint32 // listens of this node and its descendants
	native     bool   // "@" drawn by a Clack widget (<Original> or a registered host)
	hostFn     HostComponent
	host       Host
	nprops     Props     // n.P plus "children" (the rendered children) for natives
	rctx       renderCtx // curRender while this native draws

	// topLayer: a modal <dialog> (showModal): positioned against the
	// window and painted above everything else in its tree, over
	// backdrop (its ::backdrop), which covers the window.
	topLayer bool
	backdrop *box

	// up is the CSS parent of an extra tree's root laid out inside the
	// elements a native entered (nativeAnchor); nil: parent.
	up css.Element

	tc  *textCache // text node shaping cache
	svg *svgDoc

	// layout output, device px. x, y is the border box relative to the
	// layout parent lp's border box (before relative offsets and scrolling).
	x, y               float32
	w, h               float32
	m, pad, bd         [4]float32 // used margins, padding, borders (t r b l)
	mtc, mbc           float32    // collapsed top/bottom margins (block flow)
	relX, relY         float32
	base, lastBase     float32 // first/last baseline from the border-box top; <0 none
	contentW, contentH float32 // scrollable overflow size
	runs               []*run
	runPool            []*run
	lp                 *box // layout parent
	staticX, staticY   float32
	staticLP           *box
	fixedVP            bool // fixed to the viewport
	layered            bool // painted by its stacking context
	ctxRoot            bool // establishes a stacking context
	neg, pos           []*box
	fitems             []fitem
	flines             []fline

	// layout caches
	last    lc
	lastGen uint32
	lastOK  bool
	lastRes lres
	mcache  [4]measEntry
	mnext   uint8

	// natives
	natCons     consKey // final constraints
	natLaid     dimsF   // size the layout used
	natCall     op.CallOp
	natDrawn    bool
	natLive     bool // natDrawn with an input source (not a measure pass)
	natFrame    uint64
	natDrawCons consKey
	natReal     dimsF
	natMeas     [4]natMeas
	natMeasFr   uint64
	natMeasN    uint8

	// paint output (scope coordinates)
	ax, ay float32
	vis    bool
	rect   rectF
	rectFr uint64
}

type rectF struct{ x0, y0, x1, y1 float32 }

func (r rectF) empty() bool { return r.x1 <= r.x0 || r.y1 <= r.y0 }

func (r rectF) contains(x, y float32) bool { return x >= r.x0 && x < r.x1 && y >= r.y0 && y < r.y1 }

func (r rectF) inter(o rectF) rectF {
	if o.x0 > r.x0 {
		r.x0 = o.x0
	}
	if o.y0 > r.y0 {
		r.y0 = o.y0
	}
	if o.x1 < r.x1 {
		r.x1 = o.x1
	}
	if o.y1 < r.y1 {
		r.y1 = o.y1
	}
	return r
}

func (r rectF) union(o rectF) rectF {
	if r.empty() {
		return o
	}
	if o.empty() {
		return r
	}
	if o.x0 < r.x0 {
		r.x0 = o.x0
	}
	if o.y0 < r.y0 {
		r.y0 = o.y0
	}
	if o.x1 > r.x1 {
		r.x1 = o.x1
	}
	if o.y1 > r.y1 {
		r.y1 = o.y1
	}
	return r
}

func (r rectF) off(dx, dy float32) rectF { return rectF{r.x0 + dx, r.y0 + dy, r.x1 + dx, r.y1 + dy} }

// --- css.Element ---

func (b *box) Tag() string {
	switch b.kind {
	case kElem:
		return b.n.T
	case kComp:
		return "div"
	}
	return ""
}

func (b *box) ID() string {
	if b.n == nil {
		return ""
	}
	return b.n.ID
}

func (b *box) HasClass(c string) bool {
	if b.n == nil {
		return false
	}
	for _, x := range b.n.Class {
		if x == c {
			return true
		}
	}
	return false
}

func (b *box) Classes() []string {
	if b.n == nil {
		return nil
	}
	return b.n.Class
}

func (b *box) Attr(name string) (string, bool) {
	if b.n == nil {
		return "", false
	}
	return b.n.AttrString(name)
}

func (b *box) Parent() css.Element {
	if b.up != nil {
		return b.up
	}
	p := b.parent
	for p != nil && (p.kind == kAnon || p.kind == kPseudo) {
		p = p.parent
	}
	if p != nil {
		return p
	}
	if b.v != nil && b.v.topEl != nil {
		return b.v.topEl
	}
	return nil
}

func (b *box) Prev() css.Element {
	if b.n == nil {
		return nil
	}
	var sibs []*box
	if b.parent != nil {
		sibs = b.parent.kids
	} else if b.v != nil {
		sibs = b.v.top
	}
	var prev *box
	for _, s := range sibs {
		if s == b {
			break
		}
		if s.kind == kElem || s.kind == kComp {
			prev = s
		}
	}
	if prev == nil {
		return nil
	}
	return prev
}

func (b *box) Index() (int, int) {
	if b.n == nil {
		return 0, 0
	}
	return b.n.index, b.n.nsib
}

func (b *box) TypeIndex() (int, int) {
	if b.n == nil {
		return 0, 0
	}
	return b.n.tindex, b.n.ntsib
}

func (b *box) State() css.State { return b.state }

// Modal reports an open <dialog> JS showed with showModal() (:modal).
func (b *box) Modal() bool {
	if b.kind != kElem || b.n == nil || !b.n.Modal || b.n.T != "dialog" {
		return false
	}
	_, open := b.n.AttrString("open")
	return open
}

func (b *box) Kids() []css.Element {
	if !b.ekidsOK {
		b.ekidsOK = true
		for _, k := range b.kids {
			if k.kind == kElem || k.kind == kComp {
				b.ekids = append(b.ekids, k)
			}
		}
	}
	return b.ekids
}

// HasText reports text children (so :empty doesn't match).
func (b *box) HasText() bool {
	for _, k := range b.kids {
		if k.kind == kText && k.text != "" {
			return true
		}
	}
	return false
}

// isElem reports a host element or component (event targets, CSS).
func (b *box) isElem() bool { return b.kind == kElem || b.kind == kComp }

// k returns the node's host id (0 for text/generated boxes).
func (b *box) k() int64 {
	if b.n == nil {
		return 0
	}
	return b.n.K
}

// elem returns the nearest element box at or above b.
func (b *box) elem() *box {
	for b != nil && !b.isElem() {
		b = b.parent
	}
	return b
}

// --- tree building ---

// listened event groups.
const (
	lMove = 1 << iota
	lWheel
	lKey
	lHover
)

func listenBits(on map[string]int64) uint32 {
	var l uint32
	for name := range on {
		switch strings.TrimSuffix(name, "capture") {
		case "mousemove", "pointermove":
			l |= lMove
		case "wheel":
			l |= lWheel
		case "keydown", "keyup", "keypress":
			l |= lKey
		case "mouseenter", "mouseleave", "mouseover", "mouseout", "pointerenter", "pointerleave", "pointerover", "pointerout":
			l |= lHover
		}
	}
	return l
}

func (v *View) buildBoxes(nodes []*Node, parent *box) []*box {
	out := make([]*box, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, v.buildBox(n, parent))
	}
	return out
}

func (v *View) buildBox(n *Node, parent *box) *box {
	b := &box{n: n, parent: parent, v: v}
	switch {
	case n.IsText():
		b.kind = kText
		b.text = n.V
		return b
	case n.IsComponent():
		b.kind = kComp
	default:
		b.kind = kElem
		b.repl = replFor(n)
	}
	b.listens = listenBits(n.On)
	if b.repl != rSVG { // svg children are drawn by the svg renderer
		b.kids = v.buildBoxes(n.C, b)
	}
	b.subListens = b.listens
	for _, k := range b.kids {
		b.subListens |= k.subListens
	}
	if v.byK != nil && n.K != 0 {
		v.byK[n.K] = b
	}
	return b
}

func replFor(n *Node) replKind {
	switch n.T {
	case "img":
		return rImg
	case "svg":
		return rSVG
	case "br":
		return rBr
	case "textarea":
		return rTextarea
	case "select":
		return rSelect
	case "progress", "meter":
		return rProgress
	case "iframe", "video", "canvas", "object", "embed", "audio":
		return rEmbed
	case "input":
		t, _ := n.AttrString("type")
		switch strings.ToLower(t) {
		case "checkbox":
			return rCheck
		case "radio":
			return rRadio
		case "range":
			return rRange
		case "hidden":
			return rNone
		case "button", "submit", "reset":
			return rNone
		}
		return rInput
	}
	return rNone
}

// --- styles ---

// styleHook lets tests supply computed styles directly (the CSS engine
// may be a stub). It returns ok=false to fall back to the cascade.
var styleHook func(n *Node, parent *css.Style) (css.Style, bool)

// inheritOnly is the initial style with parent's inherited properties.
func inheritOnly(parent *css.Style, fg color.NRGBA) css.Style {
	s := css.Initial(fg)
	if parent == nil {
		return s
	}
	p := parent
	s.Visibility = p.Visibility
	s.Color = p.Color
	s.FontFamily = p.FontFamily
	s.FontSize = p.FontSize
	s.FontWeight = p.FontWeight
	s.Italic = p.Italic
	s.LineHeight = p.LineHeight
	s.LetterSpacing = p.LetterSpacing
	s.WordSpacing = p.WordSpacing
	s.TextAlign = p.TextAlign
	s.TextTransform = p.TextTransform
	s.WhiteSpace = p.WhiteSpace
	s.WordBreakAll = p.WordBreakAll
	s.TextShadow = p.TextShadow
	s.Cursor = p.Cursor
	s.PointerEvents = p.PointerEvents
	s.UserSelect = p.UserSelect
	s.Fill, s.FillNone = p.Fill, p.FillNone
	s.Stroke, s.StrokeNone, s.StrokeWidth = p.Stroke, p.StrokeNone, p.StrokeWidth
	s.Vars = p.Vars
	return s
}

// restyle recomputes every box's style (tree, sheet, state or env change).
func (v *View) restyle() {
	v.styleGen++
	e := cssEnv()
	v.cenv = e
	pst := v.topStyle
	if pst == nil {
		v.rootStyle = css.Initial(e.Fg)
		pst = &v.rootStyle
	}
	v.main.root.st = inheritOnly(pst, e.Fg)
	v.main.root.st.Display = css.DisplayBlock
	v.svgCSS = nil
	for _, b := range v.top {
		v.styleBox(b, pst, &e)
	}
	for _, x := range v.extra {
		for _, b := range x.boxes {
			if b.parent != nil {
				v.styleBox(b, &b.parent.st, &e)
			} else {
				v.styleBox(b, pst, &e)
			}
		}
	}
}

func (v *View) styleBox(b *box, parent *css.Style, e *css.Env) {
	if b.kind == kText {
		return
	}
	v.computeStyle(b, parent, e)
	li := 0
	for _, k := range b.kids {
		v.styleBox(k, &b.st, e)
		if k.kind != kText && k.st.Display == css.DisplayListItem {
			k.listIndex = li
			li++
		}
	}
	v.buildLayoutKids(b, e)
	if b.repl == rSVG {
		v.svgStyles(b, e)
	}
}

func (v *View) computeStyle(b *box, parent *css.Style, e *css.Env) {
	n := b.n
	b.ph = nil
	b.before, b.after = nil, nil
	b.topLayer, b.backdrop = false, nil
	b.listMarker = 0
	b.key = mix(uint64(n.K)*0x9e3779b97f4a7c15 ^ v.root<<17 ^ uint64(v.styleGen)<<1 ^ uint64(b.n.index))
	b.native, b.hostFn = false, nil
	if b.kind == kComp {
		if _, ok := SlotOf(n); ok {
			b.native = true
		} else if v.name != "" && n.N == v.name {
			b.native = true
		} else if fn, ok := LookupHost(n.N); ok {
			b.native, b.hostFn = true, fn
		}
	}
	if styleHook != nil {
		if st, ok := styleHook(n, parent); ok {
			b.st = st
			b.set = 0
			b.cursor = st.Cursor
			v.uaPost(b, nil, parent)
			return
		}
	}
	if Styles.Empty() && len(n.Style) == 0 {
		// No CSS anywhere: the UA style alone.
		b.st = inheritOnly(parent, e.Fg)
		b.set = 0
		v.uaDefaults(b, nil, parent)
		v.uaPost(b, nil, parent)
		v.topLayerStyle(b, e)
		return
	}
	c := Styles.Cascade(b, css.PseudoNone, n.Style, *e)
	stripNative(&c)
	b.st = css.Compute(&c, parent, *e)
	b.set = propsOf(&c)
	v.uaDefaults(b, &c, parent)
	if !Styles.Empty() {
		if bc := Styles.Cascade(b, css.PseudoBefore, nil, *e); len(bc.Decls) > 0 {
			if s := css.Compute(&bc, &b.st, *e); s.HasContent && s.Display != css.DisplayNone {
				b.before = v.pseudoBox(b, s)
			}
		}
		if ac := Styles.Cascade(b, css.PseudoAfter, nil, *e); len(ac.Decls) > 0 {
			if s := css.Compute(&ac, &b.st, *e); s.HasContent && s.Display != css.DisplayNone {
				b.after = v.pseudoBox(b, s)
			}
		}
		if b.repl == rInput || b.repl == rTextarea {
			if pc := Styles.Cascade(b, css.PseudoPlaceholder, nil, *e); len(pc.Decls) > 0 {
				s := css.Compute(&pc, &b.st, *e)
				b.ph = &s
			}
		}
		if n.T == "li" || b.st.Display == css.DisplayListItem {
			if lt, ok := c.Get("list-style-type"); ok {
				b.listMarker = markerFor(lt, -1)
			} else if ls, ok := c.Get("list-style"); ok {
				b.listMarker = markerFor(ls, -1)
			} else {
				b.listMarker = -1
			}
		}
	}
	v.uaPost(b, &c, parent)
	v.topLayerStyle(b, e)
}

// topLayerStyle puts an open modal dialog in the top layer and computes
// its ::backdrop (UA: a 10% black veil over the window). ::backdrop
// inherits from no element.
func (v *View) topLayerStyle(b *box, e *css.Env) {
	if !b.Modal() || b.st.Display == css.DisplayNone {
		return
	}
	b.topLayer = true
	if b.st.Position != css.PosFixed {
		// top layer boxes are absolutely positioned against the window
		b.st.Position = css.PosFixed
	}
	var bs css.Style
	var c *css.Cascaded
	if !Styles.Empty() {
		bc := Styles.Cascade(b, css.PseudoBackdrop, nil, *e)
		c = &bc
		bs = css.Compute(&bc, nil, *e)
	} else {
		bs = css.Initial(e.Fg)
	}
	if !has(c, "background", "background-color") {
		bs.Background.Color = color.NRGBA{A: 0x1a}
	}
	b.backdrop = &box{kind: kPseudo, parent: b, v: v, st: bs}
}

func markerFor(v string, def int8) int8 {
	switch {
	case strings.Contains(v, "none"):
		return 0
	case strings.Contains(v, "decimal"):
		return 2
	case strings.Contains(v, "circle"):
		return 3
	case strings.Contains(v, "square"):
		return 4
	case strings.Contains(v, "disc"):
		return 1
	}
	return def
}

func (v *View) pseudoBox(b *box, s css.Style) *box {
	p := &box{kind: kPseudo, parent: b, v: v, st: s}
	t := &box{kind: kText, parent: p, v: v, text: s.Content}
	p.kids = []*box{t}
	p.lk = p.kids
	return p
}

// has reports whether the cascade set prop (nil cascade: nothing set).
func has(c *css.Cascaded, props ...string) bool {
	if c == nil {
		return false
	}
	for _, p := range props {
		if c.Has(p) {
			return true
		}
	}
	return false
}

// uaDefaults applies the user-agent stylesheet for properties no rule set.
func (v *View) uaDefaults(b *box, c *css.Cascaded, parent *css.Style) {
	st := &b.st
	if b.kind == kComp {
		if !has(c, "display") {
			st.Display = css.DisplayInlineBlock
			if blockRoot(b.n.N) {
				st.Display = css.DisplayBlock
			}
		}
		return
	}
	t := b.n.T
	if !has(c, "display") {
		st.Display = uaDisplay(t, b.repl)
	}
	em := st.FontSize
	setM := func(top, bottom float32) {
		if !has(c, "margin", "margin-top", "margin-block", "margin-block-start") {
			st.Margin[0] = css.Px(top)
		}
		if !has(c, "margin", "margin-bottom", "margin-block", "margin-block-end") {
			st.Margin[2] = css.Px(bottom)
		}
	}
	bold := func() {
		if !has(c, "font-weight", "font") {
			st.FontWeight = 700
		}
	}
	size := func(f float32) {
		if !has(c, "font-size", "font") {
			ps := float32(15)
			if parent != nil {
				ps = parent.FontSize
			}
			st.FontSize = ps * f
			em = st.FontSize
		}
	}
	switch t {
	case "b", "strong", "th":
		bold()
	case "i", "em", "cite", "var", "dfn", "address":
		if !has(c, "font-style", "font") {
			st.Italic = true
		}
	case "h1":
		size(2)
		bold()
		setM(0.67*em, 0.67*em)
	case "h2":
		size(1.5)
		bold()
		setM(0.83*em, 0.83*em)
	case "h3":
		size(1.17)
		bold()
		setM(em, em)
	case "h4":
		bold()
		setM(1.33*em, 1.33*em)
	case "h5":
		size(0.83)
		bold()
		setM(1.67*em, 1.67*em)
	case "h6":
		size(0.67)
		bold()
		setM(2.33*em, 2.33*em)
	case "p", "dl":
		setM(em, em)
	case "blockquote", "figure":
		setM(em, em)
		if !has(c, "margin", "margin-left", "margin-inline", "margin-inline-start") {
			st.Margin[3] = css.Px(40)
		}
		if !has(c, "margin", "margin-right", "margin-inline", "margin-inline-end") {
			st.Margin[1] = css.Px(40)
		}
	case "ul", "ol", "menu":
		if parent == nil || parent.Display != css.DisplayListItem {
			setM(em, em)
		}
		if !has(c, "padding", "padding-left", "padding-inline", "padding-inline-start") {
			st.Padding[3] = css.Px(40)
		}
	case "dd":
		if !has(c, "margin", "margin-left", "margin-inline", "margin-inline-start") {
			st.Margin[3] = css.Px(40)
		}
	case "pre":
		setM(em, em)
		if !has(c, "white-space") {
			st.WhiteSpace = css.WSPre
		}
		fallthrough
	case "code", "kbd", "samp", "tt":
		if !has(c, "font-family", "font") {
			st.FontFamily = "monospace"
		}
	case "small":
		size(0.83)
	case "big":
		size(1.2)
	case "sub", "sup":
		size(0.83)
		if !has(c, "vertical-align") {
			if t == "sub" {
				st.VerticalAlign = css.VASub
			} else {
				st.VerticalAlign = css.VASuper
			}
		}
	case "u", "ins":
		if !has(c, "text-decoration", "text-decoration-line") {
			st.TextDecoration |= css.DecoUnderline
		}
	case "s", "strike", "del":
		if !has(c, "text-decoration", "text-decoration-line") {
			st.TextDecoration |= css.DecoLineThrough
		}
	case "center":
		if !has(c, "text-align") {
			st.TextAlign = css.TextCenter
		}
	case "a":
		if _, ok := b.n.AttrString("href"); ok {
			if !has(c, "color") {
				if th := env().Theme; th != nil {
					st.Color = th.C.Link
				}
			}
			if !has(c, "cursor") {
				st.Cursor = css.CursorPointer
			}
		}
	case "button":
		if !has(c, "cursor") {
			st.Cursor = css.CursorPointer
		}
		if !has(c, "text-align") {
			st.TextAlign = css.TextCenter
		}
	case "hr":
		setM(0.5*em, 0.5*em)
		if !has(c, "border", "border-top", "border-width", "border-top-width", "border-style", "border-color") {
			col := color.NRGBA{A: 0x30}
			if th := env().Theme; th != nil {
				col = th.C.Border
			}
			for i := 0; i < 4; i++ {
				st.BorderWidth[i] = 0
			}
			st.BorderWidth[0] = 1
			st.BorderStyle[0] = css.BorderSolid
			st.BorderColor[0] = col
		}
	case "input", "textarea", "select":
		if b.repl == rInput || b.repl == rTextarea || b.repl == rSelect {
			if !has(c, "border", "border-width", "border-style", "border-color", "border-top", "border-top-width") {
				col := color.NRGBA{A: 0x50}
				if th := env().Theme; th != nil {
					col = th.C.Border
				}
				for i := 0; i < 4; i++ {
					st.BorderWidth[i] = 1
					st.BorderStyle[i] = css.BorderSolid
					st.BorderColor[i] = col
				}
			}
			if !has(c, "padding", "padding-top", "padding-left", "padding-right", "padding-bottom", "padding-inline", "padding-block") {
				st.Padding = [4]css.Length{css.Px(2), css.Px(4), css.Px(2), css.Px(4)}
			}
		}
		if !has(c, "cursor") {
			switch b.repl {
			case rInput, rTextarea:
				st.Cursor = css.CursorText
			case rCheck, rRadio, rRange, rSelect:
				st.Cursor = css.CursorDefault
			}
		}
		if b.repl == rTextarea && !has(c, "white-space") {
			st.WhiteSpace = css.WSPreWrap
		}
	case "label":
		if !has(c, "cursor") {
			st.Cursor = css.CursorDefault
		}
	case "dialog":
		v.uaDialog(b, c, em)
	}
}

// uaDialog is the HTML UA stylesheet for <dialog>: closed ones aren't
// rendered; an open one is absolutely positioned, centred across its
// containing block, sized to its content, with a border and 1em of
// padding on Canvas colours; a modal one (:modal) is fixed, centred both
// ways in the window, at most the window less 2em + 6px, and scrolls.
//
//	dialog:not([open]) { display: none }
//	dialog { position: absolute; inset-inline: 0; width: fit-content; height: fit-content;
//	         margin: auto; border: solid; padding: 1em; background: Canvas; color: CanvasText }
//	dialog:modal { position: fixed; overflow: auto; inset-block: 0;
//	               max-width: calc(100% - 6px - 2em); max-height: calc(100% - 6px - 2em) }
func (v *View) uaDialog(b *box, c *css.Cascaded, em float32) {
	st := &b.st
	if _, open := b.n.AttrString("open"); !open {
		if !has(c, "display") {
			st.Display = css.DisplayNone
		}
		return
	}
	modal := b.Modal()
	if !has(c, "position") {
		st.Position = css.PosAbsolute
		if modal {
			st.Position = css.PosFixed
		}
	}
	if !has(c, "inset", "inset-inline", "inset-inline-start", "left") {
		st.Left = css.Zero
	}
	if !has(c, "inset", "inset-inline", "inset-inline-end", "right") {
		st.Right = css.Zero
	}
	if modal {
		if !has(c, "inset", "inset-block", "inset-block-start", "top") {
			st.Top = css.Zero
		}
		if !has(c, "inset", "inset-block", "inset-block-end", "bottom") {
			st.Bottom = css.Zero
		}
		if !has(c, "overflow", "overflow-x") {
			st.OverflowX = css.OverflowAuto
		}
		if !has(c, "overflow", "overflow-y") {
			st.OverflowY = css.OverflowAuto
		}
		limit := css.Length{Pct: 100, Px: -(2*em + 6)}
		if !has(c, "max-width", "max-inline-size") {
			st.MaxWidth = limit
		}
		if !has(c, "max-height", "max-block-size") {
			st.MaxHeight = limit
		}
	}
	if !has(c, "width", "inline-size") {
		st.Width = css.Length{Kind: css.LenFitContent}
	}
	if !has(c, "height", "block-size") {
		st.Height = css.Length{Kind: css.LenFitContent}
	}
	sides := [4][]string{
		{"margin", "margin-top", "margin-block", "margin-block-start"},
		{"margin", "margin-right", "margin-inline", "margin-inline-end"},
		{"margin", "margin-bottom", "margin-block", "margin-block-end"},
		{"margin", "margin-left", "margin-inline", "margin-inline-start"},
	}
	for i, props := range sides {
		if !has(c, props...) {
			st.Margin[i] = css.Auto
		}
	}
	pads := [4][]string{
		{"padding", "padding-top", "padding-block", "padding-block-start"},
		{"padding", "padding-right", "padding-inline", "padding-inline-end"},
		{"padding", "padding-bottom", "padding-block", "padding-block-end"},
		{"padding", "padding-left", "padding-inline", "padding-inline-start"},
	}
	for i, props := range pads {
		if !has(c, props...) {
			st.Padding[i] = css.Px(em)
		}
	}
	th := env().Theme
	if !has(c, "color") && th != nil {
		st.Color = th.C.Fg
	}
	if !has(c, "border", "border-width", "border-style", "border-color", "border-top", "border-right", "border-bottom", "border-left",
		"border-top-width", "border-right-width", "border-bottom-width", "border-left-width",
		"border-top-style", "border-right-style", "border-bottom-style", "border-left-style") {
		for i := 0; i < 4; i++ {
			st.BorderWidth[i] = 3 // medium
			st.BorderStyle[i] = css.BorderSolid
			st.BorderColor[i] = st.Color
		}
	}
	if !has(c, "background", "background-color") && th != nil {
		st.Background.Color = th.C.Bg
	}
}

// uaPost fixes up style facts the layout engine relies on.
func (v *View) uaPost(b *box, c *css.Cascaded, parent *css.Style) {
	st := &b.st
	if st.Display == css.DisplayListItem && b.listMarker == 0 && c == nil {
		b.listMarker = -1
	}
	if b.listMarker == -1 { // inherit from the list
		b.listMarker = 1
		if p := b.parent; p != nil && p.n != nil {
			switch p.n.T {
			case "ol":
				b.listMarker = 2
			}
			if p.listMarker == 0 && p.n.T != "ul" && p.n.T != "ol" {
				b.listMarker = 0
			}
		}
	}
	if b.n != nil && truthy(b.n.P["hidden"]) && !has(c, "display") {
		st.Display = css.DisplayNone
	}
	if b.kind == kElem && b.n.T == "input" {
		if t, _ := b.n.AttrString("type"); strings.EqualFold(t, "hidden") {
			st.Display = css.DisplayNone
		}
	}
	b.cursor = st.Cursor
}

func uaDisplay(t string, r replKind) css.Display {
	switch t {
	case "div", "p", "section", "article", "header", "footer", "nav", "main", "aside", "ul", "ol", "menu",
		"form", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "pre", "figure", "figcaption", "hr",
		"dl", "dt", "dd", "fieldset", "details", "summary", "address", "center", "legend", "dialog", "html", "body", "hgroup", "search":
		return css.DisplayBlock
	case "li":
		return css.DisplayListItem
	case "table":
		return css.DisplayTable
	case "tr":
		return css.DisplayTableRow
	case "td", "th":
		return css.DisplayTableCell
	case "thead", "tbody", "tfoot", "caption", "colgroup":
		return css.DisplayBlock
	case "head", "script", "style", "template", "title", "meta", "link", "noscript", "base", "col", "datalist", "param", "source", "track", "option", "optgroup":
		return css.DisplayNone
	case "button", "input", "select", "textarea", "progress", "meter":
		return css.DisplayInlineBlock
	}
	return css.DisplayInline
}

// isInlineLevel reports whether a box with display d takes part in its
// parent's inline formatting context.
func isInlineLevel(d css.Display) bool {
	switch d {
	case css.DisplayInline, css.DisplayInlineBlock, css.DisplayInlineFlex, css.DisplayInlineGrid:
		return true
	}
	return false
}

func isFlex(d css.Display) bool { return d == css.DisplayFlex || d == css.DisplayInlineFlex }
func isGrid(d css.Display) bool { return d == css.DisplayGrid || d == css.DisplayInlineGrid }

// outOfFlow reports absolute and fixed boxes.
func outOfFlow(st *css.Style) bool {
	return st.Position == css.PosAbsolute || st.Position == css.PosFixed
}

// buildLayoutKids fills b.lk: pseudos and kids, with display:contents
// flattened and, in flex/grid containers, runs of text wrapped in
// anonymous blocks.
func (v *View) buildLayoutKids(b *box, e *css.Env) {
	b.lk = b.lk[:0]
	b.ekidsOK = false
	b.ekids = b.ekids[:0]
	add := func(k *box) { b.lk = append(b.lk, k) }
	if b.before != nil {
		add(b.before)
	}
	var walk func(kids []*box)
	walk = func(kids []*box) {
		for _, k := range kids {
			if k.kind != kText && k.st.Display == css.DisplayContents {
				if k.before != nil {
					add(k.before)
				}
				walk(k.kids)
				if k.after != nil {
					add(k.after)
				}
				continue
			}
			add(k)
		}
	}
	walk(b.kids)
	if b.after != nil {
		add(b.after)
	}
	if b.kind == kText || !(isFlex(b.st.Display) && b.st.LineClamp == 0 || isGrid(b.st.Display) || b.st.Display == css.DisplayTableRow) {
		return
	}
	// Flex/grid: wrap consecutive text runs into anonymous blocks; drop
	// whitespace-only runs.
	out := b.lk[:0:0]
	var run []*box
	flush := func() {
		if len(run) == 0 {
			return
		}
		ws := true
		for _, t := range run {
			if strings.TrimSpace(t.text) != "" {
				ws = false
			}
		}
		if !ws || b.st.WhiteSpace == css.WSPre || b.st.WhiteSpace == css.WSPreWrap {
			a := &box{kind: kAnon, parent: b, v: v, st: inheritOnly(&b.st, e.Fg)}
			a.st.Display = css.DisplayBlock
			a.kids = append([]*box(nil), run...)
			a.lk = a.kids
			out = append(out, a)
		}
		run = run[:0]
	}
	for _, k := range b.lk {
		if k.kind == kText {
			run = append(run, k)
			continue
		}
		flush()
		out = append(out, k)
	}
	flush()
	b.lk = out
}
