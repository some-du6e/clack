package slackc

import (
	"image"
	"image/color"
	"math"
	"strconv"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
)

// View lays out one root's host trees and keeps per-node UI state (hover,
// focus, editors, scroll offsets) keyed by Node.K across trees.
type View struct {
	root    uint64
	overlay bool
	sheet   string // text of the tree's <style> elements, installed as "root:<id>"

	fr   frame
	name string

	nodes []*Node
	top   []*box
	byK   map[int64]*box
	ns    map[int64]*nodeState
	main  scope
	subs  map[any]*scope
	extra map[*Node]*extraTree

	topEl     css.Element
	topKey    uint64
	hasTop    bool
	rootStyle css.Style
	topStyle  *css.Style
	cenv      css.Env

	styleGen  uint32
	styleVer  uint64
	styleDark bool
	styleWin  image.Point
	needStyle bool
	dirtyGen  uint32 // bumps force a relayout (images arriving, state)

	dp, sp, vw, vh float32
	maxH           int
	pendingImg     bool
	frameNo        uint64
	layGen         uint32 // generation of the scope being laid out
	genSeq         uint32

	// events
	hover    []*box // innermost first
	hoverK   int64
	pressK   int64
	pressBtn pointer.Buttons
	activeK  []int64
	focusK   int64
	seen     [16]evSig
	seenN    int
	lastClk  struct {
		k int64
		t int64
		n int
	}
	dragK    int64
	rangeK   int64
	chainBuf []*box
	filtBuf  []css.Filter
	tip      tipState
	svgCSS   map[*Node]*svgCSS
	svgStyFn func(*Node) *svgCSS
	cmap     colorMap
	cmapFn   func(color.NRGBA) color.NRGBA
}

// colorMap applies the color filters in effect (for the svg renderer).
type colorMap struct{ filt []css.Filter }

func (m *colorMap) apply(c color.NRGBA) color.NRGBA {
	for i := len(m.filt) - 1; i >= 0; i-- {
		c = applyFilter(m.filt[i], c)
	}
	return c
}

// frame is the per-Layout-call context. It must not outlive the call:
// native and orig are cleared before Layout returns.
type frame struct {
	name   string
	orig   Props
	native Native
}

// nodeState is a host node's UI state that survives new trees.
type nodeState struct {
	k                int64
	st               css.State
	scrollX, scrollY float32
	hover            bool
	dragging         bool
	dragOff          float32
	thumb            rectF
	ed               *widget.Editor
	edNode           *Node
	sent             []string
	lastText         string
	checked          bool
	checkedSet       bool
	value            string
	valueSet         bool
	open             bool // select dropdown
	rangeV           float64
	rangeDrag        bool
	focused          bool
	autoFocused      bool
	jsFocused        bool // a JS focus() in a modal dialog was applied
	tag              int  // focus tag for non-editor focusables (address used)
	filters          []event.Filter
	focusVal         string // input value when focused (change fires on blur if it differs)
	chkNode          *Node
	host             any
	rect             [4]float32
	rectSent         bool
}

// extraTree is a React-element prop's nodes (not part of the tree).
type extraTree struct {
	boxes []*box
	used  uint64
	up    css.Element // the roots' CSS parent (nil: the patched component's box)
	upKey uint64
}

// scope is one independent layout root: the View's tree, or a subtree a
// host component lays out (Host.Children, Element, Nodes, text).
type scope struct {
	v     *View
	root  box
	host  *box // the "@" box of a sub scope (nil for the main scope)
	boxes []*box

	gtx  layout.Context
	key  scopeKey
	ok   bool
	gen  uint32
	dims layout.Dimensions

	natives []*box
	hits    []hitEntry
	painted []*box
	bounds  rectF

	ptag, ctag, stag int // pointer handler, cursor areas, shields (addresses used)

	origin     f32.Point
	originOK   bool
	ptr        f32.Point
	ptrIn      bool
	used       uint64
	exact      bool
	src        css.Style // the style a sub scope's root inherits from
	srcOK      bool
	rectsDirty bool
	rehover    bool
	dropdown   dropState
}

type scopeKey struct {
	cons     layout.Constraints
	styleGen uint32
	dirtyGen uint32
	dp, sp   float32
	win      image.Point
}

// NewView returns the view for root.
func NewView(root uint64) *View {
	v := &View{root: root, byK: map[int64]*box{}, ns: map[int64]*nodeState{}}
	v.svgStyFn = v.svgSty
	v.cmapFn = v.cmap.apply
	v.main.v = v
	v.main.root.kind = kAnon
	v.main.root.v = v
	return v
}

func (v *View) setMetrics(gtx layout.Context) {
	v.dp = gtx.Metric.PxPerDp
	if v.dp == 0 {
		v.dp = 1
	}
	v.sp = gtx.Metric.PxPerSp
	if v.sp == 0 {
		v.sp = v.dp
	}
	ws := windowSize
	if ws == (image.Point{}) {
		ws = gtx.Constraints.Max
		if ws.X > 1<<20 {
			ws.X = 1920
		}
		if ws.Y > 1<<20 {
			ws.Y = 1080
		}
	}
	v.vw, v.vh = float32(ws.X), float32(ws.Y)
	v.maxH = gtx.Constraints.Max.Y
}

// Layout lays out nodes; "@" nodes named name call native.
func (v *View) Layout(gtx layout.Context, nodes []*Node, name string, orig Props, native Native) layout.Dimensions {
	v.fr = frame{name: name, orig: orig, native: native}
	v.frameNo++
	v.setMetrics(gtx)
	if name != v.name {
		v.name = name
		v.needStyle = true
	}
	if !sameNodes(v.nodes, nodes) {
		v.setTree(nodes)
	}
	el, st, key := stackParent()
	if key != v.topKey || (st != nil) != v.hasTop {
		v.topKey, v.hasTop = key, st != nil
		if st != nil {
			v.rootStyle = *st
			v.topStyle = &v.rootStyle
		} else {
			v.topStyle = nil
		}
		v.needStyle = true
	}
	v.topEl = el
	if ver := Styles.Version(); ver != v.styleVer {
		v.styleVer = ver
		v.needStyle = true
	}
	dark := env().Theme != nil && env().Theme.Dark
	if dark != v.styleDark {
		v.styleDark = dark
		v.needStyle = true
	}
	if !Styles.Empty() && windowSize != v.styleWin {
		v.styleWin = windowSize
		v.needStyle = true
	}
	if v.needStyle {
		v.needStyle = false
		v.restyle()
	}
	if v.pendingImg {
		v.pendingImg = false
		v.dirtyGen++
	}
	s := &v.main
	s.exact = v.overlay
	dims := s.run(gtx)
	v.drawTip(gtx)
	v.sweep()
	v.topEl = nil
	v.fr = frame{}
	return dims
}

func sameNodes(a, b []*Node) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return a != nil || b == nil
}

// setTree installs a new delivered tree.
func (v *View) setTree(nodes []*Node) {
	v.nodes = nodes
	v.syncSheet(nodes)
	clear(v.byK)
	v.top = v.buildBoxes(nodes, nil)
	v.main.boxes = v.top
	v.main.root.lk = v.top
	v.main.ok = false
	v.subs = nil
	v.extra = nil
	v.svgCSS = nil
	for k, ns := range v.ns {
		b := v.byK[k]
		if b == nil {
			delete(v.ns, k)
			continue
		}
		b.ns = ns
		b.state = ns.st
	}
	v.hover = v.hover[:0]
	if b := v.byK[v.hoverK]; b != nil {
		for x := b; x != nil; x = x.parent {
			if x.isElem() {
				v.hover = append(v.hover, x)
			}
		}
	}
	v.needStyle = true
}

// state returns (creating) the node state for b.
func (v *View) state(b *box) *nodeState {
	if b.ns != nil {
		return b.ns
	}
	k := b.k()
	if k == 0 {
		b.ns = &nodeState{}
		return b.ns
	}
	ns := v.ns[k]
	if ns == nil {
		ns = &nodeState{k: k}
		v.ns[k] = ns
	}
	b.ns = ns
	return ns
}

// Close releases the view's state.
func (v *View) Close() {
	if v.sheet != "" {
		v.sheet = ""
		RemoveStyle(v.sheetKey())
	}
	v.nodes = nil
	v.top = nil
	clear(v.byK)
	clear(v.ns)
	v.subs = nil
	v.extra = nil
	v.hover = nil
	v.main = scope{v: v}
	v.fr = frame{}
	v.topEl = nil
}

func (v *View) hostState(k int64, mk func() any) any {
	b := v.byK[k]
	var ns *nodeState
	if b != nil {
		ns = v.state(b)
	} else {
		ns = v.ns[k]
		if ns == nil {
			ns = &nodeState{k: k}
			v.ns[k] = ns
		}
	}
	if ns.host == nil {
		ns.host = mk()
	}
	return ns.host
}

// sweep drops sub scopes and element trees not drawn for a while.
func (v *View) sweep() {
	if v.frameNo%64 != 0 {
		return
	}
	for k, s := range v.subs {
		if v.frameNo-s.used > 120 {
			delete(v.subs, k)
		}
	}
	for k, x := range v.extra {
		if v.frameNo-x.used > 120 {
			delete(v.extra, k)
		}
	}
}

// --- scopes ---

func (s *scope) windowOrigin() (float32, float32) {
	if s.v.overlay && s.host == nil {
		return 0, 0
	}
	if s.originOK {
		return s.origin.X, s.origin.Y
	}
	if s.host != nil && s.host.scope != nil && s.host.scope != s {
		hx, hy := s.host.scope.windowOrigin()
		return hx + s.host.ax + s.host.bd[3] + s.host.pad[3], hy + s.host.ay + s.host.bd[0] + s.host.pad[0]
	}
	return 0, 0
}

func (s *scope) run(gtx layout.Context) layout.Dimensions {
	v := s.v
	s.gtx = gtx
	s.used = v.frameNo
	saveGen := v.layGen
	key := scopeKey{cons: gtx.Constraints, styleGen: v.styleGen, dirtyGen: v.dirtyGen, dp: v.dp, sp: v.sp, win: windowSize}
	if !s.ok || key != s.key {
		s.key = key
		s.layout()
	}
	v.layGen = s.gen
	for pass := 0; pass < 3; pass++ {
		if !s.drawNatives() {
			break
		}
		s.layout()
	}
	v.layGen = saveGen
	s.paint()
	s.events()
	s.flushRects()
	s.gtx = layout.Context{}
	return s.dims
}

// layout lays out the scope's tree under s.gtx's constraints.
func (s *scope) layout() {
	v := s.v
	saveGen := v.layGen
	v.genSeq++
	s.gen = v.genSeq
	v.layGen = s.gen
	s.ok = true
	s.rectsDirty = true
	cs := s.gtx.Constraints
	root := &s.root
	root.scope = s
	root.lp = nil
	for _, b := range s.boxes {
		setScope(b, s)
	}
	root.lk = flatContents(s.boxes)
	maxW := float32(cs.Max.X)
	if cs.Max.X >= 1<<24 {
		maxW = inf
	}
	maxH := float32(cs.Max.Y)
	exact := s.exact || cs.Min.Y == cs.Max.Y && cs.Max.Y < 1<<24
	c := lc{availW: maxW, cbW: maxW, cbH: -1, bfc: true}
	if exact {
		c.cbH = maxH
		c.fixedH, c.fixH = true, maxH
	}
	// A tree of inline-level content (a patched sender name, a badge)
	// is as wide as its content; block-level roots fill the width like
	// they would in their DOM parent.
	if s.exact || !s.inlineRoot() && maxW < inf/2 {
		c.fixedW, c.fixW = true, maxW
	} else {
		c.shrink = true
	}
	if s.root.st.FontSize == 0 {
		s.root.st = css.Initial(cssEnv().Fg)
	}
	root.st.Display = css.DisplayBlock
	root.lastOK = false
	v.layout(root, c)
	if !s.exact && root.w < float32(cs.Min.X) {
		c.shrink = false
		c.fixedW, c.fixW = true, float32(cs.Min.X)
		v.layout(root, c)
	}
	s.natives = s.natives[:0]
	s.postLayout()
	w := ceilI(root.w)
	h := ceilI(root.h)
	if h < cs.Min.Y {
		h = cs.Min.Y
	}
	if w < cs.Min.X {
		w = cs.Min.X
	}
	if w > cs.Max.X {
		w = cs.Max.X
	}
	if h > cs.Max.Y {
		h = cs.Max.Y
	}
	s.dims = layout.Dimensions{Size: image.Pt(w, h)}
	if root.base >= 0 {
		s.dims.Baseline = h - int(math.Round(float64(root.base)))
	}
	v.layGen = saveGen
}

// flatContents is a scope's top-level boxes with display:contents ones
// replaced by their layout kids, as buildLayoutKids does below the root:
// a contents wrapper around a tree's top (Slick ChatTabs' layout div)
// leaves its children laid out against the scope, so percentages and
// flex sizes resolve as in a browser. boxes itself when there are none.
func flatContents(boxes []*box) []*box {
	n := -1
	for i, b := range boxes {
		if b.kind != kText && b.st.Display == css.DisplayContents {
			n = i
			break
		}
	}
	if n < 0 {
		return boxes
	}
	out := append(make([]*box, 0, len(boxes)+4), boxes[:n]...)
	for _, b := range boxes[n:] {
		if b.kind != kText && b.st.Display == css.DisplayContents {
			out = append(out, b.lk...) // its pseudos and kids, contents already flattened
			continue
		}
		out = append(out, b)
	}
	return out
}

// inlineRoot reports a scope whose in-flow top-level boxes are all
// inline-level.
func (s *scope) inlineRoot() bool {
	for _, b := range s.root.lk {
		if b.kind == kText {
			continue
		}
		if b.st.Display == css.DisplayNone || outOfFlow(&b.st) {
			continue
		}
		if !isInlineLevel(b.st.Display) {
			return false
		}
	}
	return true
}

func setScope(b *box, s *scope) {
	b.scope = s
	if b.native {
		return // its children belong to the host's sub scopes
	}
	for _, k := range b.lk {
		setScope(k, s)
	}
}

func (s *scope) addNative(b *box) { s.natives = append(s.natives, b) }

// measureNative measures a native widget under cs without delivering
// input. This frame's real draw under the same constraints is reused.
func (s *scope) measureNative(b *box, cs layout.Constraints) dimsF {
	v := s.v
	k := ck(cs)
	if b.natDrawn && b.natFrame == v.frameNo && b.natDrawCons == k {
		return b.natReal
	}
	if b.natMeasFr != v.frameNo {
		b.natMeasFr = v.frameNo
		for i := range b.natMeas {
			b.natMeas[i].ok = false
		}
	}
	for i := range b.natMeas {
		if b.natMeas[i].ok && b.natMeas[i].ck == k {
			return b.natMeas[i].d
		}
	}
	g := s.gtx.Disabled()
	g.Constraints = cs
	m := op.Record(g.Ops)
	d := s.callNative(g, b)
	m.Stop()
	df := dimsOf(d)
	e := &b.natMeas[b.natMeasN%uint8(len(b.natMeas))]
	b.natMeasN++
	e.ok, e.ck, e.d = true, k, df
	return df
}

func dimsOf(d layout.Dimensions) dimsF {
	base := float32(-1)
	if d.Baseline > 0 {
		base = float32(d.Size.Y - d.Baseline)
	}
	return dimsF{float32(d.Size.X), float32(d.Size.Y), base}
}

// drawNatives draws every native widget once for real (recorded, placed
// by paint). It reports whether some size differs from the layout's.
func (s *scope) drawNatives() bool {
	v := s.v
	changed := false
	for _, b := range s.natives {
		// Reuse this frame's draw unless it ran without input (inside a
		// parent native's measure pass, gtx.Disabled()) and this pass has
		// input: natives nested under a patch must see their events.
		live := s.gtx.Source.Enabled()
		if b.natDrawn && b.natFrame == v.frameNo && b.natDrawCons == b.natCons && (b.natLive || !live) {
			continue
		}
		g := s.gtx
		g.Constraints = b.natCons.cons()
		m := op.Record(g.Ops)
		d := s.callNative(g, b)
		b.natCall = m.Stop()
		b.natDrawn, b.natFrame, b.natDrawCons, b.natLive = true, v.frameNo, b.natCons, live
		b.natReal = dimsOf(d)
		if b.natReal != b.natLaid {
			changed = true
		}
	}
	return changed
}

// callNative calls the widget behind an "@" box with its host ancestors
// on the element stack.
func (s *scope) callNative(gtx layout.Context, b *box) layout.Dimensions {
	v := s.v
	var chain [64]*box
	n := 0
	// Climb to the root of b's tree, or to the root of the extra tree it
	// belongs to when that root is anchored (up): the elements above an
	// anchored root are the ones already on the stack.
	for p := b; p.up == nil && n < len(chain); {
		if p = p.parent; p == nil {
			break
		}
		if p.isElem() {
			chain[n] = p
			n++
		}
	}
	for i := n - 1; i >= 0; i-- {
		p := chain[i]
		pushHost(p, &p.st, p.set, p.key)
	}
	var d layout.Dimensions
	prev := curRender
	b.rctx = renderCtx{view: v, fr: &v.fr, st: &b.st, n: b.n, depth: len(stack)}
	curRender = &b.rctx
	switch slot, isSlot := SlotOf(b.n); {
	case isSlot:
		if slot.Native != nil {
			d = slot.Native(gtx, nativeProps(b))
		}
	case b.n.N == v.fr.name && v.fr.name != "":
		if v.fr.native != nil {
			d = v.fr.native(gtx, nativeProps(b))
		}
	case b.hostFn != nil:
		b.host = Host{Node: b.n, Props: b.n.P, Style: &b.st, view: v, fr: &v.fr}
		d = b.hostFn(gtx, &b.host)
	}
	curRender = prev
	for i := 0; i < n; i++ {
		popHost()
	}
	return d
}

// nativeProps is what a native gets from <Original {...props}>{children}:
// the props, plus the rendered children as an Element under "children"
// (PropChildren) when the plugin passed any. Built once per tree.
func nativeProps(b *box) Props {
	n := b.n
	if len(n.C) == 0 {
		return n.P
	}
	if b.nprops == nil {
		p := make(Props, len(n.P)+1)
		for k, x := range n.P {
			p[k] = x
		}
		p["children"] = Element{Nodes: n.C}
		b.nprops = p
	}
	return b.nprops
}

// paint paints the scope at the current op origin and registers its
// input areas.
func (s *scope) paint() {
	gtx := s.gtx
	p := painter{v: s.v, s: s, gtx: gtx, ops: gtx.Ops, clip: noClip}
	s.hits = s.hits[:0]
	s.painted = s.painted[:0]
	s.bounds = rectF{}
	p.filt = s.v.filtBuf[:0]
	p.paintBox(&s.root, 0, 0)
	s.v.filtBuf = p.filt[:0]
	s.drawDropdown(&p)
	// The scope's pointer handler sits on top of everything it drew,
	// pass-through, so natives below get their events and so do we.
	if !s.bounds.empty() {
		r := irect(s.bounds)
		ar := clip.Rect(r).Push(gtx.Ops)
		pp := pointer.PassOp{}.Push(gtx.Ops)
		event.Op(gtx.Ops, &s.ptag)
		pp.Pop()
		ar.Pop()
	}
}

// --- host component entry points (host.go) ---

func (v *View) boxOf(n *Node) *box {
	if n == nil {
		return nil
	}
	if n.K != 0 {
		if b := v.byK[n.K]; b != nil && b.n == n {
			return b
		}
	}
	for _, x := range v.extra {
		for _, b := range x.boxes {
			if r := findBox(b, n); r != nil {
				return r
			}
		}
	}
	for _, b := range v.top {
		if r := findBox(b, n); r != nil {
			return r
		}
	}
	return nil
}

func findBox(b *box, n *Node) *box {
	if b.n == n {
		return b
	}
	for _, k := range b.kids {
		if r := findBox(k, n); r != nil {
			return r
		}
	}
	return nil
}

type subKey struct {
	b    *box
	kind uint8
	ptr  any
}

func (v *View) sub(key subKey, host *box, st *css.Style) *scope {
	if v.subs == nil {
		v.subs = map[any]*scope{}
	}
	s := v.subs[key]
	if s == nil {
		s = &scope{v: v, host: host}
		s.root.kind = kAnon
		s.root.v = v
		v.subs[key] = s
	}
	if st != nil && (!s.srcOK || !sameStyleShallow(&s.src, st)) {
		s.src, s.srcOK = *st, true
		s.root.st = inheritOnly(st, cssEnv().Fg)
		s.root.st.Display = css.DisplayBlock
		s.ok = false
	}
	return s
}

// sameStyleShallow compares the fields a sub root inherits.
func sameStyleShallow(a, b *css.Style) bool {
	return a.Color == b.Color && a.FontSize == b.FontSize && a.FontWeight == b.FontWeight && a.Italic == b.Italic &&
		a.LineHeight == b.LineHeight && a.WhiteSpace == b.WhiteSpace && a.TextAlign == b.TextAlign &&
		a.FontFamily == b.FontFamily && a.LetterSpacing == b.LetterSpacing && a.Display == b.Display &&
		a.FlexDirection == b.FlexDirection && a.AlignItems == b.AlignItems && a.JustifyContent == b.JustifyContent &&
		a.ColGap == b.ColGap && a.RowGap == b.RowGap && a.TextTransform == b.TextTransform && a.Cursor == b.Cursor
}

func (v *View) runSub(gtx layout.Context, s *scope, host *box) layout.Dimensions {
	if v.dp == 0 {
		v.setMetrics(gtx)
	}
	if host != nil {
		pushHost(host, &host.st, host.set, host.key)
		defer popHost()
	}
	return s.run(gtx)
}

func (v *View) layoutKids(gtx layout.Context, fr *frame, n *Node, st *css.Style) layout.Dimensions {
	b := v.boxOf(n)
	if b == nil {
		return layout.Dimensions{}
	}
	s := v.sub(subKey{b: b, kind: 1}, b, st)
	if s.boxes == nil || len(s.boxes) != len(b.lk) || len(b.lk) > 0 && &s.boxes[0] != &b.lk[0] {
		s.boxes = b.lk
		s.root.lk = b.lk
		s.ok = false
	}
	return v.runSub(gtx, s, b)
}

func (v *View) layoutNodes(gtx layout.Context, fr *frame, nodes []*Node, parent *Node, st *css.Style) layout.Dimensions {
	if len(nodes) == 0 {
		return layout.Dimensions{}
	}
	pb := v.boxOf(parent)
	if v.extra == nil {
		v.extra = map[*Node]*extraTree{}
	}
	up, upKey := nativeAnchor()
	x := v.extra[nodes[0]]
	restyled := x == nil || x.up != up || x.upKey != upKey
	if x == nil {
		x = &extraTree{boxes: v.buildBoxes(nodes, pb)}
		v.extra[nodes[0]] = x
	}
	if restyled {
		x.up, x.upKey = up, upKey
		for _, b := range x.boxes {
			b.up = up
		}
		e := cssEnv()
		ps := st
		if ps == nil && pb != nil {
			ps = &pb.st
		}
		if ps == nil {
			s := css.Initial(e.Fg)
			ps = &s
		}
		for _, b := range x.boxes {
			v.styleBox(b, ps, &e)
		}
	}
	x.used = v.frameNo
	s := v.sub(subKey{b: pb, kind: 2, ptr: nodes[0]}, pb, st)
	if s.boxes == nil || restyled {
		s.boxes = x.boxes
		s.root.lk = x.boxes
		s.ok = false
	}
	if up != nil {
		// The stack already ends in the native's own elements; pushing
		// pb would put the patched component's host chain on top of them.
		return v.runSub(gtx, s, nil)
	}
	return v.runSub(gtx, s, pb)
}

// nativeAnchor is the CSS parent for nodes a native lays out after
// entering its own elements (ClassDef.Enter), as Slack's DOM has them:
// TextyButtons' Original draws .c-texty_buttons and lays the
// TextyButtonOverflow children out inside it, so SlimMessageBox's
// `.c-texty_buttons [data-qa=emoji_toolbar_button]` reaches the emoji
// button. nil when the native entered nothing (or none is drawing): the
// nodes keep the patched component's box as their parent.
func nativeAnchor() (css.Element, uint64) {
	c := curRender
	if c == nil || len(stack) <= c.depth {
		return nil, 0
	}
	e := &stack[len(stack)-1]
	return e.el, e.key
}

type textKey struct {
	st *css.Style
	s  string
}

func (v *View) layoutText(gtx layout.Context, fr *frame, txt string, st *css.Style) layout.Dimensions {
	s := v.sub(subKey{kind: 3, ptr: textKey{st, txt}}, nil, st)
	if s.boxes == nil {
		t := &box{kind: kText, text: txt, v: v, parent: &s.root}
		s.boxes = []*box{t}
		s.root.lk = s.boxes
		s.ok = false
	}
	return v.runSub(gtx, s, nil)
}

// --- rects for getBoundingClientRect / ResizeObserver ---

var pendingRects map[int64][4]float32

func (s *scope) flushRects() {
	if !s.rectsDirty || bridge() == nil {
		return
	}
	s.rectsDirty = false
	v := s.v
	ox, oy := s.windowOrigin()
	for _, b := range s.painted {
		k := b.k()
		if k == 0 || b.rectFr != v.frameNo {
			continue
		}
		r := b.rect
		w := [4]float32{(r.x0 + ox) / v.dp, (r.y0 + oy) / v.dp, (r.x1 - r.x0) / v.dp, (r.y1 - r.y0) / v.dp}
		ns := v.state(b)
		if ns.rectSent && ns.rect == w {
			continue
		}
		ns.rect, ns.rectSent = w, true
		if pendingRects == nil {
			pendingRects = map[int64][4]float32{}
		}
		pendingRects[k] = w
	}
}

func flushRects(b Bridge) {
	if len(pendingRects) == 0 {
		return
	}
	r := pendingRects
	pendingRects = nil
	b.SetRects(r)
}

// syncSheet installs the text of <style> elements a plugin renders inside
// its tree as a sheet that lives as long as the root (React apps put
// their CSS there).
func (v *View) syncSheet(nodes []*Node) {
	var sb []byte
	var walk func(ns []*Node)
	walk = func(ns []*Node) {
		for _, n := range ns {
			if n.T == "style" {
				for _, c := range n.C {
					if c.IsText() {
						sb = append(sb, c.V...)
					}
				}
				sb = append(sb, '\n')
				continue
			}
			if !n.IsText() {
				walk(n.C)
			}
		}
	}
	walk(nodes)
	if string(sb) == v.sheet {
		return
	}
	v.sheet = string(sb)
	if v.sheet == "" {
		RemoveStyle(v.sheetKey())
	} else {
		SetStyle(v.sheetKey(), v.sheet)
	}
}

func (v *View) sheetKey() string { return "root:" + strconv.FormatUint(v.root, 10) }
