package slackc

import (
	"image"
	"image/color"
	"strings"
	"sync"

	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/slackc/css"
)

// Styles is the CSS engine for api.setStyle, DOM <style> tags plugins add
// and the user CSS box (key "user").
var Styles = css.NewEngine()

// SetStyle installs or replaces the sheet under key (any goroutine).
func SetStyle(key, src string) {
	Styles.Set(key, src)
	resetNativeStyles()
	invalidate()
}

// RemoveStyle drops the sheet under key.
func RemoveStyle(key string) {
	Styles.Remove(key)
	resetNativeStyles()
	invalidate()
}

// ClassDef is a native widget's Slack identity: its class names, tag and
// static attributes, declared once per widget kind at package level.
//
//	var msgClass = slackc.Class("c-message_kit__message", "data-qa", "message_container").
//		Mods("c-message_kit__message--hover", "c-message--highlight")
type ClassDef struct {
	tag     string
	classes []string
	attrs   []string // name, value pairs
	mods    []string
	kids    []*ClassDef // Kids: elements the native draws inside itself
	// flexItems: host trees right under it are its flex items (FlexItems)
	flexItems bool
}

// Class declares a native widget class. classes is a space-separated
// class list; attrs are name/value pairs (data-qa, role, aria-label…).
func Class(classes string, attrs ...string) *ClassDef {
	return &ClassDef{tag: "div", classes: strings.Fields(classes), attrs: attrs}
}

// Tag sets the element's tag name (default "div"), e.g. "button", "span".
func (d *ClassDef) Tag(tag string) *ClassDef { d.tag = tag; return d }

// Mods declares optional modifier classes; Enter's mods bit i adds mods[i].
func (d *ClassDef) Mods(classes ...string) *ClassDef { d.mods = classes; return d }

// Kids declares the elements Slack's DOM has inside the widget that the
// native draws itself without entering them (an icon, a name), each with
// its own declared kids, so :has() and :empty see them: a private
// channel's InlineChannelEntity holds span.c-inline_channel_icon
// [data-inline-channel-type-icon=lock], which StreamerMode's
// .c-inline_channel_entity:has([data-inline-channel-type-icon^="lock"])
// looks for. A widget whose children differ by state (a private or a
// public channel) declares one ClassDef per shape. Natives without
// declared kids still answer :has() with false and :empty with no.
func (d *ClassDef) Kids(kids ...*ClassDef) *ClassDef { d.kids = kids; return d }

// FlexItems marks a native that is a flex container in Slack's DOM whose
// children include what plugins render in it: a patched component's
// top-level nodes, laid out right under it, become its flex items under
// its computed flex properties (the top nav's right container, where
// StreamerMode's HelpButton patch puts its toggle beside Slack's help
// button), instead of the blocks of a plain container.
func (d *ClassDef) FlexItems() *ClassDef { d.flexItems = true; return d }

// flexParent reports a native marked FlexItems.
func flexParent(el css.Element) bool {
	switch e := el.(type) {
	case *nativeEl:
		return e.def.flexItems
	case *kidsEl:
		return e.def.flexItems
	}
	return false
}

// WithKids is a copy of d that also declares kids: the shape of a widget
// whose children depend on its state (the composer box with its pending
// files), cached by the caller like any ClassDef.
func (d *ClassDef) WithKids(kids ...*ClassDef) *ClassDef {
	c := *d
	c.kids = append(append([]*ClassDef(nil), d.kids...), kids...)
	return &c
}

// Styled is the style a native widget gets from CSS. The zero value (no
// CSS active, or nothing matched in the chain) answers every query with
// the widget's own default.
type Styled struct {
	s *nativeStyle
}

// nativeStyle is one cached element: cascade + computed style.
type nativeStyle struct {
	el     nativeEl
	kel    *kidsEl // el with its declared kids (ClassDef.Kids), nil without
	style  css.Style
	set    propSet // properties a rule set on this element
	inh    propSet // inherited properties some rule set on it or an ancestor
	native propSet // properties Slack's own rule (base sheet) keeps native here
	trans  bool    // some transition-* declaration applies (transitions.go)
	deco   decoProp
	key    uint64
	before *css.Style
	after  *css.Style
}

// decoProp is the text decoration an element draws on its text: its own
// text-decoration and what its ancestors propagate to it (CSS decorations
// aren't inherited, but every in-flow inline descendant's text gets them).
type decoProp struct {
	lines css.TextDecoration // line bits and DecoWavy
	col   color.NRGBA        // zero: the text's own color (currentColor)
}

func (d decoProp) merge(own decoProp) decoProp {
	if own.lines.Lines() == 0 {
		return d
	}
	if d.lines.Lines() == 0 {
		return own
	}
	return decoProp{lines: d.lines | own.lines, col: own.col}
}

// ownDeco is the decoration st (an element some rule styled) starts.
func ownDeco(st *css.Style, colorSet bool) decoProp {
	if st == nil || st.TextDecoration.Lines() == 0 {
		return decoProp{}
	}
	d := decoProp{lines: st.TextDecoration, col: st.DecoColor}
	if d.col.A == 0 && colorSet {
		d.col = st.Color
	}
	return d
}

// atomicTag reports elements that are atomic inlines (decorations don't
// propagate into them).
func atomicTag(tag string) bool { return tag == "button" || tag == "img" || tag == "input" }

type nativeEl struct {
	def    *ClassDef
	mods   uint64
	st     css.State
	attrK  string
	attrV  string
	extra  []string
	parent css.Element
}

func (e *nativeEl) Tag() string { return e.def.tag }
func (e *nativeEl) ID() string  { return e.attr("id") }
func (e *nativeEl) HasClass(c string) bool {
	for _, x := range e.def.classes {
		if x == c {
			return true
		}
	}
	for i, x := range e.def.mods {
		if e.mods&(1<<uint(i)) != 0 && x == c {
			return true
		}
	}
	for _, x := range e.extra {
		if x == c {
			return true
		}
	}
	return false
}
func (e *nativeEl) Classes() []string {
	if e.mods == 0 && len(e.extra) == 0 {
		return e.def.classes
	}
	out := append([]string(nil), e.def.classes...)
	out = append(out, e.extra...)
	for i, x := range e.def.mods {
		if e.mods&(1<<uint(i)) != 0 {
			out = append(out, x)
		}
	}
	return out
}
func (e *nativeEl) attr(name string) string {
	v, _ := e.Attr(name)
	return v
}
func (e *nativeEl) Attr(name string) (string, bool) {
	switch name {
	case "class":
		return strings.Join(e.Classes(), " "), true
	}
	if e.attrK != "" && e.attrK == name {
		return e.attrV, true
	}
	for i := 0; i+1 < len(e.def.attrs); i += 2 {
		if e.def.attrs[i] == name {
			return e.def.attrs[i+1], true
		}
	}
	return "", false
}
func (e *nativeEl) Parent() css.Element {
	if e.parent == nil {
		return nil
	}
	return e.parent
}
func (e *nativeEl) Prev() css.Element     { return nil }
func (e *nativeEl) Index() (int, int)     { return 0, 0 }
func (e *nativeEl) TypeIndex() (int, int) { return 0, 0 }
func (e *nativeEl) State() css.State      { return e.st }

// kidsEl is a native with declared kids: a css.KidsElement over them.
type kidsEl struct {
	*nativeEl
	kids []css.Element
}

func (k *kidsEl) Kids() []css.Element { return k.kids }

// declEl is a declared kid (ClassDef.Kids): its def's tag, classes and
// static attributes under its parent, and its own declared kids.
type declEl struct {
	def    *ClassDef
	parent css.Element
	kids   []css.Element
	index  int
	n      int
}

func (e *declEl) Tag() string { return e.def.tag }
func (e *declEl) ID() string  { v, _ := e.Attr("id"); return v }
func (e *declEl) HasClass(c string) bool {
	for _, x := range e.def.classes {
		if x == c {
			return true
		}
	}
	return false
}
func (e *declEl) Classes() []string { return e.def.classes }
func (e *declEl) Attr(name string) (string, bool) {
	if name == "class" {
		return strings.Join(e.def.classes, " "), true
	}
	for i := 0; i+1 < len(e.def.attrs); i += 2 {
		if e.def.attrs[i] == name {
			return e.def.attrs[i+1], true
		}
	}
	return "", false
}
func (e *declEl) Parent() css.Element { return e.parent }
func (e *declEl) Prev() css.Element {
	if k, ok := e.parent.(css.KidsElement); ok && e.index > 0 {
		return k.Kids()[e.index-1]
	}
	return nil
}
func (e *declEl) Index() (int, int)     { return e.index, e.n }
func (e *declEl) TypeIndex() (int, int) { return 0, 0 }
func (e *declEl) State() css.State {
	if len(e.kids) == 0 {
		return css.StateEmpty
	}
	return 0
}
func (e *declEl) Kids() []css.Element { return e.kids }

// declKids builds the declared kids of def under parent.
func declKids(def *ClassDef, parent css.Element) []css.Element {
	if len(def.kids) == 0 {
		return nil
	}
	out := make([]css.Element, len(def.kids))
	for i, kd := range def.kids {
		k := &declEl{def: kd, parent: parent, index: i, n: len(def.kids)}
		k.kids = declKids(kd, k)
		out[i] = k
	}
	return out
}

// element is the cached native as selectors see it.
func (ns *nativeStyle) element() css.Element {
	if ns.kel != nil {
		return ns.kel
	}
	return &ns.el
}

// The element stack (UI goroutine): natives and host-tree nodes push
// themselves so selectors see the real ancestry.
type stackEntry struct {
	el    css.Element
	style *css.Style
	inh   propSet
	deco  decoProp
	key   uint64
}

var (
	stack []stackEntry

	nsMu      sync.Mutex
	nsVersion uint64
	nsCache   = map[nsKey]*nativeStyle{}
	nsPrev    map[nsKey]*nativeStyle // the previous generation (lookupNative)
	nsSeq     uint64                 // natives computed: every key is new
	nsDark    bool
)

type nsKey struct {
	def    *ClassDef
	mods   uint64
	st     css.State
	attrK  string
	attrV  string
	extra  string
	parent uint64
}

// maxNativeStyles is one generation of the native style cache. Natives
// under per-row host nodes (and their parts) add entries per message row,
// each with its computed styles (~4 KB): one generation of 32,768 held
// 77 MB after a long channel was scrolled, two of 4,096 still 36 MB. Two
// generations of 2,048 cover every native a frame draws (a few hundred),
// which a lookup promotes to the current one.
const maxNativeStyles = 1 << 11

func resetNativeStyles() {
	nsMu.Lock()
	nsCache = map[nsKey]*nativeStyle{}
	nsPrev = nil
	nsMu.Unlock()
}

// Enter tags a native widget for this frame; pair every Enter with Leave
// (also on early returns). With no CSS active it costs one atomic load
// and returns the zero Styled.
func (d *ClassDef) Enter(gtx layout.Context, st css.State, mods uint64) Styled {
	if probe.active {
		probeDisplay(uaDisplay(d.tag, rNone))
	}
	if Styles.Empty() {
		return Styled{}
	}
	return d.enter(st, mods, "", "")
}

// EnterClass is Enter with extra classes known only at run time, such as
// the className a plugin passed to <Original className=…> (StreamerMode,
// IdvStatus, OwlMode). extra is a space-separated list; "" is Enter.
func (d *ClassDef) EnterClass(gtx layout.Context, st css.State, mods uint64, extra string) Styled {
	if probe.active {
		probeDisplay(uaDisplay(d.tag, rNone))
	}
	if Styles.Empty() {
		return Styled{}
	}
	return d.enterX(st, mods, "", "", extra)
}

// EnterAttr is Enter with one dynamic attribute, e.g.
// ("data-qa-channel-sidebar-channel-id", "C123").
func (d *ClassDef) EnterAttr(gtx layout.Context, st css.State, mods uint64, name, value string) Styled {
	if probe.active {
		probeDisplay(uaDisplay(d.tag, rNone))
	}
	if Styles.Empty() {
		return Styled{}
	}
	return d.enter(st, mods, name, value)
}

func (d *ClassDef) enter(st css.State, mods uint64, ak, av string) Styled {
	return d.enterX(st, mods, ak, av, "")
}

func (d *ClassDef) enterX(st css.State, mods uint64, ak, av, extra string) Styled {
	var parent *stackEntry
	if len(stack) > 0 {
		parent = &stack[len(stack)-1]
	}
	k := nsKey{def: d, mods: mods, st: st, attrK: ak, attrV: av, extra: extra}
	if parent != nil {
		k.parent = parent.key
	}
	ns := lookupNative(k, parent)
	stack = append(stack, ns.entry())
	return Styled{s: ns}
}

func (ns *nativeStyle) entry() stackEntry {
	return stackEntry{el: ns.element(), style: &ns.style, inh: ns.inh, deco: ns.deco, key: ns.key}
}

// Child is the style of a native part drawn inside s that isn't pushed on
// the element stack: a run of text, a link or a list item a native lays
// out itself (richview's message body). It matches selectors and inherits
// exactly like an element nested in s ("s d"), and is cached like Enter.
// The zero Styled (no CSS) has zero children.
func (s Styled) Child(d *ClassDef, mods uint64) Styled {
	if s.s == nil {
		return Styled{}
	}
	pe := s.s.entry()
	return Styled{s: lookupNative(nsKey{def: d, mods: mods, parent: pe.key}, &pe)}
}

// Scope is the innermost element on the stack, the parent of the natives
// a widget is about to draw (see Styled.Child). Zero while no CSS is
// installed.
type Scope struct {
	e   stackEntry
	ver uint64
	ok  bool
}

// CurrentScope captures the element stack's top (one atomic load while no
// CSS is installed).
func CurrentScope() Scope {
	if Styles.Empty() {
		return Scope{}
	}
	sc := Scope{ver: Styles.Version(), ok: true}
	if n := len(stack); n > 0 {
		sc.e = stack[n-1]
	}
	return sc
}

// BodyScope is the scope of <body>: the parent of what Slack renders in
// a portal (tooltips, menus), which a row's rules don't reach. Zero
// while no CSS is installed.
func BodyScope() Scope {
	if Styles.Empty() {
		return Scope{}
	}
	sc := Scope{ver: Styles.Version(), ok: true}
	for i := range stack {
		if el := stack[i].el; el != nil && el.Tag() == "body" {
			sc.e = stack[i]
			break
		}
		if i > 1 {
			break
		}
	}
	return sc
}

// Same reports that two scopes give their children the same styles (the
// same element in the same stylesheet generation).
func (sc Scope) Same(o Scope) bool {
	return sc.ok == o.ok && sc.ver == o.ver && sc.e.key == o.e.key && sc.e.el == o.e.el
}

// Active reports that CSS is installed (the scope can style children).
func (sc Scope) Active() bool { return sc.ok }

// Child is Styled.Child under the scope's element.
func (sc Scope) Child(d *ClassDef, mods uint64) Styled {
	if !sc.ok {
		return Styled{}
	}
	var pe *stackEntry
	if sc.e.el != nil {
		pe = &sc.e
	}
	return Styled{s: lookupNative(nsKey{def: d, mods: mods, parent: sc.e.key}, pe)}
}

func lookupNative(k nsKey, parent *stackEntry) *nativeStyle {
	ver := Styles.Version()
	dark := env().Theme != nil && env().Theme.Dark
	nsMu.Lock()
	if ver != nsVersion || dark != nsDark {
		nsCache = map[nsKey]*nativeStyle{}
		nsPrev = nil
		nsVersion, nsDark = ver, dark
	}
	ns := nsCache[k]
	if ns == nil {
		if ns = nsPrev[k]; ns != nil {
			nsCache[k] = ns // still drawn: into the current generation
		}
	}
	nsMu.Unlock()
	if ns != nil {
		return ns
	}
	ns = &nativeStyle{el: nativeEl{def: k.def, mods: k.mods, st: k.st, attrK: k.attrK, attrV: k.attrV, extra: strings.Fields(k.extra)}}
	if len(k.def.kids) > 0 {
		ns.kel = &kidsEl{nativeEl: &ns.el}
		ns.kel.kids = declKids(k.def, ns.kel)
	}
	var ps *css.Style
	var pinh propSet
	var pdeco decoProp
	if parent != nil {
		ns.el.parent = parent.el
		ps = parent.style
		pinh = parent.inh
		pdeco = parent.deco
	}
	e := cssEnv()
	el := ns.element()
	c := Styles.Cascade(el, css.PseudoNone, nil, e)
	ns.style = css.Compute(&c, ps, e)
	if vs := scopeVarsOf(&ns.el, ns.style.Vars); vs != nil {
		ns.style.Vars = vs // a theme scope's tokens (scopevars.go)
	}
	ns.set, ns.native = propsOf(&c), nativesOf(&c)
	ns.trans = hasTransition(&c)
	// An inherited property Slack's own rule gives the widget (its link
	// color, its bold name) stops an ancestor's value here, for its
	// descendants too.
	ns.inh = (pinh&^ns.native | ns.set) & inheritedMask
	if atomicTag(k.def.tag) {
		pdeco = decoProp{}
	}
	ns.deco = pdeco.merge(ownDeco(&ns.style, ns.set&propBits["color"] != 0))
	if b := Styles.Cascade(el, css.PseudoBefore, nil, e); len(b.Decls) > 0 {
		s := css.Compute(&b, &ns.style, e)
		if s.HasContent {
			ns.before = &s
		}
	}
	if a := Styles.Cascade(el, css.PseudoAfter, nil, e); len(a.Decls) > 0 {
		s := css.Compute(&a, &ns.style, e)
		if s.HasContent {
			ns.after = &s
		}
	}
	nsMu.Lock()
	// a key no other native has had (a children's lookup must never find
	// the entry of a native from an older generation)
	nsSeq++
	ns.key = mix(nsSeq*0x9e3779b97f4a7c15 ^ k.parent)
	if len(nsCache) >= maxNativeStyles {
		// natives under per-row host nodes (and their parts) add entries
		// per row: the older generation goes rather than grow without bound
		nsPrev = nsCache
		nsCache = make(map[nsKey]*nativeStyle, maxNativeStyles/2)
	}
	nsCache[k] = ns
	nsMu.Unlock()
	return ns
}

// Leave pops the widget pushed by Enter.
func (s Styled) Leave() {
	if s.s == nil {
		return
	}
	if n := len(stack); n > 0 {
		stack = stack[:n-1]
	}
}

// pushHost / popHost let the host-tree layout put its nodes on the stack.
func pushHost(el css.Element, st *css.Style, set propSet, key uint64) {
	if probe.active && st != nil {
		probeDisplay(st.Display)
	}
	var inh propSet
	var deco decoProp
	if len(stack) > 0 {
		inh = stack[len(stack)-1].inh & inheritedMask
		deco = stack[len(stack)-1].deco
	}
	if st != nil {
		switch st.Display {
		case css.DisplayInlineBlock, css.DisplayInlineFlex, css.DisplayInlineGrid:
			deco = decoProp{}
		}
		if el != nil && atomicTag(el.Tag()) {
			deco = decoProp{}
		}
		deco = deco.merge(ownDeco(st, set&propBits["color"] != 0))
	}
	stack = append(stack, stackEntry{el: el, style: st, inh: inh | set&inheritedMask, deco: deco, key: key})
}

func popHost() {
	if n := len(stack); n > 0 {
		stack = stack[:n-1]
	}
}

// EffectiveOpacity is the product of the CSS opacity of every element on
// the stack: what a native drawn now is multiplied by (host boxes and
// Styled.Wrap apply it). 1 with no CSS.
func EffectiveOpacity() float32 {
	o := float32(1)
	for i := range stack {
		if st := stack[i].style; st != nil && st.Opacity < 1 {
			o *= max(st.Opacity, 0)
		}
	}
	return o
}

// stackParent is the innermost element on the stack (nil at the root),
// the CSS parent of a host tree's top-level nodes.
func stackParent() (css.Element, *css.Style, uint64) {
	if len(stack) == 0 {
		return nil, nil, 0
	}
	e := &stack[len(stack)-1]
	return e.el, e.style, e.key
}

// cssEnv is the media environment for the current frame.
func cssEnv() css.Env {
	e := css.Env{Width: float32(windowSize.X), Height: float32(windowSize.Y), RootFont: 16}
	if th := env().Theme; th != nil {
		e.Dark = th.Dark
		e.Fg = th.C.Fg
	}
	e.RootVars = rootVars(e.Dark) // design tokens, plus the UI font's tracking (tracking.go)
	return e
}

// --- queries (all nil-safe) ---

// Active reports that some rule applies to this widget or its ancestry.
func (s Styled) Active() bool {
	// a ::before or ::after counts: a rule may give the native nothing but
	// generated content (MentionProfiles' picture before the mention's
	// name), which Wrap draws
	return s.s != nil && ((s.s.set|s.s.inh) != 0 || s.s.before != nil || s.s.after != nil)
}

// Style is the computed style, or nil when no CSS is active.
func (s Styled) Style() *css.Style {
	if s.s == nil {
		return nil
	}
	return &s.s.style
}

// Has reports whether a rule set prop ("color", "padding-left", …) on
// this widget, or (for inherited properties) on an ancestor.
func (s Styled) Has(prop string) bool {
	if s.s == nil {
		return false
	}
	b, ok := propBits[prop]
	return ok && (s.s.set|s.s.inh)&b != 0
}

// Hidden reports display:none or visibility:hidden. The widget should
// draw nothing (display:none also takes no space; see Wrap).
func (s Styled) Hidden() bool {
	if s.s == nil || !s.Active() {
		return false
	}
	return s.s.style.Display == css.DisplayNone || s.s.style.Visibility != css.Visible && s.Has("visibility")
}

// Color returns the CSS color when set (here or inherited), else def.
func (s Styled) Color(def color.NRGBA) color.NRGBA {
	if s.Has("color") {
		return s.s.style.Color
	}
	return def
}

// Background returns the CSS background color when set, else def.
func (s Styled) Background(def color.NRGBA) color.NRGBA {
	if s.Has("background-color") {
		return s.s.style.Background.Color
	}
	return def
}

// Generated is the ::before / ::after content CSS gives the widget ("" for
// none), which Wrap draws around it. Natives that report what they show
// (a sender name a plugin replaced with ::after) read it here.
func (s Styled) Generated() (before, after string) {
	if s.s == nil || !s.Active() {
		return "", ""
	}
	if b := s.s.before; b != nil && b.Display != css.DisplayNone {
		before = b.Content
	}
	if a := s.s.after; a != nil && a.Display != css.DisplayNone {
		after = a.Content
	}
	return before, after
}

// BackgroundImage is the url() of a CSS background-image set on the
// widget, else def. Image natives (avatars) draw it as their picture.
func (s Styled) BackgroundImage(def string) string {
	if s.Has("background-image") && s.s.style.Background.Image != "" {
		return s.s.style.Background.Image
	}
	return def
}

// FontSize returns the CSS font size (CSS px == Sp) when set, else def.
func (s Styled) FontSize(def unit.Sp) unit.Sp {
	if s.Has("font-size") {
		return unit.Sp(s.s.style.FontSize)
	}
	return def
}

// FontWeight returns the CSS weight (100..900) when set, else def.
func (s Styled) FontWeight(def int) int {
	if s.Has("font-weight") {
		return s.s.style.FontWeight
	}
	return def
}

// Italic returns the CSS font-style when set, else def.
func (s Styled) Italic(def bool) bool {
	if s.Has("font-style") {
		return s.s.style.Italic
	}
	return def
}

// Padding returns CSS padding (each side that a rule set) over def.
func (s Styled) Padding(def layout.Inset) layout.Inset {
	if s.s == nil {
		return def
	}
	st := &s.s.style
	side := func(i int, prop string, d unit.Dp) unit.Dp {
		if s.Has(prop) {
			return unit.Dp(st.Padding[i].Resolve(0, float32(windowSize.X), float32(windowSize.Y), float32(d)))
		}
		return d
	}
	return layout.Inset{
		Top:    side(0, "padding-top", def.Top),
		Right:  side(1, "padding-right", def.Right),
		Bottom: side(2, "padding-bottom", def.Bottom),
		Left:   side(3, "padding-left", def.Left),
	}
}

// Size returns CSS width/height (px or % of the incoming max) for the
// sides a rule set, over def.
func (s Styled) Size(gtx layout.Context, def image.Point) image.Point {
	if s.s == nil {
		return def
	}
	st := &s.s.style
	vw, vh := float32(windowSize.X), float32(windowSize.Y)
	if s.Has("width") && !st.Width.IsAuto() {
		def.X = gtx.Dp(unit.Dp(st.Width.Resolve(float32(gtx.Constraints.Max.X)/gtx.Metric.PxPerDp, vw, vh, 0)))
	}
	if s.Has("height") && !st.Height.IsAuto() {
		def.Y = gtx.Dp(unit.Dp(st.Height.Resolve(float32(gtx.Constraints.Max.Y)/gtx.Metric.PxPerDp, vw, vh, 0)))
	}
	return def
}

// Deco is the text decoration the widget's text gets: its own
// text-decoration and the ones its ancestors propagate (a struck-through
// row strikes its body). col is zero for the text's own color.
func (s Styled) Deco() (lines css.TextDecoration, col color.NRGBA) {
	if s.s == nil {
		return 0, color.NRGBA{}
	}
	return s.s.deco.lines, s.s.deco.col
}

// Opacity returns the CSS opacity (1 when unset).
func (s Styled) Opacity() float32 {
	if s.Has("opacity") {
		return s.s.style.Opacity
	}
	return 1
}

// Wrap lays out w with the box effects CSS set on the widget: display:
// none (zero size, w not called), margin, background, border, radius,
// box-shadow, opacity, filter (blur, grayscale…), transform, min/max
// sizes and ::before/::after content. Padding and text properties are the
// widget's own business (Padding, Color, FontSize…). With the zero Styled
// it is exactly w(gtx).
func (s Styled) Wrap(gtx layout.Context, w layout.Widget) layout.Dimensions {
	if s.s == nil || !s.Active() {
		return w(gtx)
	}
	if s.s.style.Display == css.DisplayNone {
		return layout.Dimensions{}
	}
	return wrapNative(gtx, s.s, w)
}

// WrapOwnPseudo is Wrap for a native that lays out its box
// pseudo-elements among its own parts (Pseudo, DrawPseudo): a mention
// pill draws a ::before picture inside its pill, as CSS puts it inside
// the slug's box. Text ::before/::after content is still Wrap's.
func (s Styled) WrapOwnPseudo(gtx layout.Context, w layout.Widget) layout.Dimensions {
	if s.s == nil || !s.Active() {
		return w(gtx)
	}
	if s.s.style.Display == css.DisplayNone {
		return layout.Dimensions{}
	}
	return wrapNativeX(gtx, s.s, w, true)
}

// Pseudo is the size in px of the native's ::before (or ::after) when it
// generates a box rather than text (content set to an empty string, with
// a size: MentionProfiles' 1em picture), margins included; ok is false
// when there's none.
func (s Styled) Pseudo(gtx layout.Context, after bool) (w, h float32, ok bool) {
	pb := s.pseudoBox(gtx, after)
	if pb == nil {
		return 0, 0, false
	}
	return pb.m[3] + pb.w + pb.m[1], pb.m[0] + pb.h + pb.m[2], true
}

// DrawPseudo paints the box Pseudo measured with its margin box at x,
// centred in a line lineH tall from y (vertical-align: middle).
func (s Styled) DrawPseudo(gtx layout.Context, after bool, x, y, lineH float32) {
	pb := s.pseudoBox(gtx, after)
	if pb == nil {
		return
	}
	p := painter{v: wrapState.v, ops: gtx.Ops, clip: noClip}
	p.wrapPseudo(pb, x, y, lineH)
}

func (s Styled) pseudoBox(gtx layout.Context, after bool) *box {
	if s.s == nil {
		return nil
	}
	st := s.s.before
	if after {
		st = s.s.after
	}
	if st == nil {
		return nil
	}
	return wrapPseudo(wrapView(gtx), st, float32(gtx.Constraints.Max.X))
}

// WrapRounded is Wrap for a native that rounds its own box (a menu, a
// popover, a modal card) as Slack's own stylesheet rounds the element
// (.c-menu{border-radius:4px}, .c-sk-modal{border-radius:8px}): a rule's
// background, border and box-shadow follow radius unless the rule sets
// border-radius itself. With the zero Styled it is exactly w(gtx).
func (s Styled) WrapRounded(gtx layout.Context, radius unit.Dp, w layout.Widget) layout.Dimensions {
	if s.s == nil || !s.Active() {
		return w(gtx)
	}
	if s.s.style.Display == css.DisplayNone {
		return layout.Dimensions{}
	}
	if s.Has("border-radius") {
		return wrapNative(gtx, s.s, w)
	}
	b := wrapBox(s.s)
	old := b.st.Radius
	for i := range b.st.Radius {
		b.st.Radius[i] = css.Px(float32(radius))
	}
	defer func() { b.st.Radius = old }()
	return wrapNative(gtx, s.s, w)
}

// BorderWidth is the top border width a rule set, in dp (0 when none).
func (s Styled) BorderWidth() unit.Dp {
	if !s.Has("border") {
		return 0
	}
	return unit.Dp(s.s.style.BorderWidth[0])
}

// propSet is a bitmask of the properties natives can query.
type propSet uint64

var propBits = map[string]propSet{}
var inheritedMask propSet

func init() {
	props := []struct {
		names []string
		inh   bool
	}{
		{[]string{"color"}, true},
		{[]string{"font-size", "font"}, true},
		{[]string{"font-weight", "font"}, true},
		{[]string{"font-style", "font"}, true},
		{[]string{"font-family", "font"}, true},
		{[]string{"line-height", "font"}, true},
		{[]string{"letter-spacing"}, true},
		{[]string{"text-transform"}, true},
		{[]string{"visibility"}, true},
		{[]string{"cursor"}, true},
		{[]string{"text-align"}, true},
		{[]string{"white-space"}, true},
		{[]string{"display"}, false},
		{[]string{"background-color", "background"}, false},
		{[]string{"padding-top", "padding", "padding-block", "padding-block-start"}, false},
		{[]string{"padding-right", "padding", "padding-inline", "padding-inline-end"}, false},
		{[]string{"padding-bottom", "padding", "padding-block", "padding-block-end"}, false},
		{[]string{"padding-left", "padding", "padding-inline", "padding-inline-start"}, false},
		{[]string{"margin-top", "margin", "margin-block", "margin-block-start"}, false},
		{[]string{"margin-right", "margin", "margin-inline", "margin-inline-end"}, false},
		{[]string{"margin-bottom", "margin", "margin-block", "margin-block-end"}, false},
		{[]string{"margin-left", "margin", "margin-inline", "margin-inline-start"}, false},
		{[]string{"width", "inline-size"}, false},
		{[]string{"height", "block-size"}, false},
		{[]string{"min-width", "max-width", "min-height", "max-height"}, false},
		{[]string{"opacity"}, false},
		{[]string{"filter"}, false},
		{[]string{"transform", "translate", "scale", "rotate"}, false},
		{[]string{"border", "border-width", "border-color", "border-style", "border-top", "border-right", "border-bottom", "border-left",
			"border-top-width", "border-right-width", "border-bottom-width", "border-left-width",
			"border-top-color", "border-right-color", "border-bottom-color", "border-left-color",
			"border-top-style", "border-right-style", "border-bottom-style", "border-left-style"}, false},
		{[]string{"border-radius", "border-top-left-radius", "border-top-right-radius", "border-bottom-left-radius", "border-bottom-right-radius"}, false},
		{[]string{"box-shadow"}, false},
		{[]string{"background-image"}, false},
		{[]string{"outline", "outline-width", "outline-style", "outline-color", "outline-offset"}, false},
		{[]string{"text-decoration", "text-decoration-line"}, false},
		{[]string{"pointer-events"}, true},
	}
	for i, p := range props {
		b := propSet(1) << uint(i)
		for _, n := range p.names {
			propBits[n] |= b
		}
		if p.inh {
			inheritedMask |= b
		}
	}
}

// propsOf is the properties a plugin or user rule set (Slack's own rules
// in the base sheet, NativeValue, aren't CSS values for the native).
func propsOf(c *css.Cascaded) propSet {
	var s propSet
	for _, d := range c.Decls {
		if b, ok := propBits[d.Prop]; ok && d.Value != css.NativeValue {
			s |= b
		}
	}
	return s
}

// nativesOf is the properties Slack's own rule won for the element.
func nativesOf(c *css.Cascaded) propSet {
	var s propSet
	for _, d := range c.Decls {
		if d.Value == css.NativeValue {
			s |= propBits[d.Prop]
		}
	}
	return s
}

// ExtraClass is the className a plugin passed to <Original className=…>
// (nil-safe; "" when unpatched), for ClassDef.EnterClass.
func ExtraClass(p Props) string {
	s, _ := p["className"].(string)
	return s
}
