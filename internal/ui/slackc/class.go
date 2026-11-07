package slackc

import (
	"image"
	"image/color"
	"strings"
	"sync"

	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
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

// Styled is the style a native widget gets from CSS. The zero value (no
// CSS active, or nothing matched in the chain) answers every query with
// the widget's own default.
type Styled struct {
	s *nativeStyle
}

// nativeStyle is one cached element: cascade + computed style.
type nativeStyle struct {
	el     nativeEl
	style  css.Style
	set    propSet // properties a rule set on this element
	inh    propSet // inherited properties some rule set on it or an ancestor
	key    uint64
	before *css.Style
	after  *css.Style
}

type nativeEl struct {
	def    *ClassDef
	mods   uint64
	st     css.State
	attrK  string
	attrV  string
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
	return false
}
func (e *nativeEl) Classes() []string {
	if e.mods == 0 {
		return e.def.classes
	}
	out := append([]string(nil), e.def.classes...)
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

// The element stack (UI goroutine): natives and host-tree nodes push
// themselves so selectors see the real ancestry.
type stackEntry struct {
	el    css.Element
	style *css.Style
	inh   propSet
	key   uint64
}

var (
	stack []stackEntry

	nsMu      sync.Mutex
	nsVersion uint64
	nsCache   = map[nsKey]*nativeStyle{}
	nsDark    bool
)

type nsKey struct {
	def    *ClassDef
	mods   uint64
	st     css.State
	attrK  string
	attrV  string
	parent uint64
}

func resetNativeStyles() {
	nsMu.Lock()
	nsCache = map[nsKey]*nativeStyle{}
	nsMu.Unlock()
}

// Enter tags a native widget for this frame; pair every Enter with Leave
// (also on early returns). With no CSS active it costs one atomic load
// and returns the zero Styled.
func (d *ClassDef) Enter(gtx layout.Context, st css.State, mods uint64) Styled {
	if Styles.Empty() {
		return Styled{}
	}
	return d.enter(st, mods, "", "")
}

// EnterAttr is Enter with one dynamic attribute, e.g.
// ("data-qa-channel-sidebar-channel-id", "C123").
func (d *ClassDef) EnterAttr(gtx layout.Context, st css.State, mods uint64, name, value string) Styled {
	if Styles.Empty() {
		return Styled{}
	}
	return d.enter(st, mods, name, value)
}

func (d *ClassDef) enter(st css.State, mods uint64, ak, av string) Styled {
	var parent *stackEntry
	if len(stack) > 0 {
		parent = &stack[len(stack)-1]
	}
	k := nsKey{def: d, mods: mods, st: st, attrK: ak, attrV: av}
	if parent != nil {
		k.parent = parent.key
	}
	ns := lookupNative(k, parent)
	stack = append(stack, stackEntry{el: &ns.el, style: &ns.style, inh: ns.inh, key: ns.key})
	return Styled{s: ns}
}

func lookupNative(k nsKey, parent *stackEntry) *nativeStyle {
	ver := Styles.Version()
	dark := env().Theme != nil && env().Theme.Dark
	nsMu.Lock()
	if ver != nsVersion || dark != nsDark {
		nsCache = map[nsKey]*nativeStyle{}
		nsVersion, nsDark = ver, dark
	}
	ns := nsCache[k]
	nsMu.Unlock()
	if ns != nil {
		return ns
	}
	ns = &nativeStyle{el: nativeEl{def: k.def, mods: k.mods, st: k.st, attrK: k.attrK, attrV: k.attrV}}
	var ps *css.Style
	var pinh propSet
	if parent != nil {
		ns.el.parent = parent.el
		ps = parent.style
		pinh = parent.inh
	}
	e := cssEnv()
	c := Styles.Cascade(&ns.el, css.PseudoNone, nil, e)
	ns.style = css.Compute(&c, ps, e)
	ns.set = propsOf(&c)
	ns.inh = pinh&inheritedMask | ns.set&inheritedMask
	if b := Styles.Cascade(&ns.el, css.PseudoBefore, nil, e); len(b.Decls) > 0 {
		s := css.Compute(&b, &ns.style, e)
		if s.HasContent {
			ns.before = &s
		}
	}
	if a := Styles.Cascade(&ns.el, css.PseudoAfter, nil, e); len(a.Decls) > 0 {
		s := css.Compute(&a, &ns.style, e)
		if s.HasContent {
			ns.after = &s
		}
	}
	ns.key = mix(uint64(len(nsCache)+1)*0x9e3779b97f4a7c15 ^ k.parent)
	nsMu.Lock()
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
	var inh propSet
	if len(stack) > 0 {
		inh = stack[len(stack)-1].inh & inheritedMask
	}
	stack = append(stack, stackEntry{el: el, style: st, inh: inh | set&inheritedMask, key: key})
}

func popHost() {
	if n := len(stack); n > 0 {
		stack = stack[:n-1]
	}
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
	e.RootVars = css.SlackVars(e.Dark)
	return e
}

// --- queries (all nil-safe) ---

// Active reports that some rule applies to this widget or its ancestry.
func (s Styled) Active() bool { return s.s != nil && (s.s.set|s.s.inh) != 0 }

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
		{[]string{"outline"}, false},
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

func propsOf(c *css.Cascaded) propSet {
	var s propSet
	for _, d := range c.Decls {
		if b, ok := propBits[d.Prop]; ok {
			s |= b
		}
	}
	return s
}
