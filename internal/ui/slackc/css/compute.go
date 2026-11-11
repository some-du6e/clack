package css

import (
	"image/color"
	"math"
	"strings"
)

// invalidVar marks a guaranteed-invalid custom property in Style.Vars.
const invalidVar = "\x00"

// cctx is the state of one Compute (or one parse-time validation).
type cctx struct {
	st         *Style
	parent     *Style // nil at the root
	env        Env
	validate   bool
	fontSizing bool // computing font-size: em = parent font size
	calcPctNum bool
	// quirkUnitless accepts unitless numbers as px (SVG presentation
	// attributes, stroke-width).
	quirkUnitless bool

	bcSet    [4]bool
	ocSet    bool
	bgImgs   []bgLayer
	bgSet    bool
	bgSize   []string
	bgPos    []string
	bgRepeat []bool
	indiv    [3]*Transform // translate, rotate, scale
	indivSet [3]bool
	xform    []Transform
	xformSet bool
	dropSh   []Shadow
	boxSh    []Shadow
	boxShSet bool
	webkit   bool // display: -webkit-box / -webkit-inline-box
	orient   byte // 'h' / 'v' from -webkit-box-orient
	rev      bool // -webkit-box-direction: reverse
	areas    int
	gridSet  bool
	wrapMode int8 // text-wrap-mode: -1 wrap, +1 nowrap
}

type bgLayer struct {
	url  string
	grad *Gradient
}

func (cx *cctx) emBase() float32 {
	if cx.fontSizing {
		if cx.parent != nil {
			return cx.parent.FontSize
		}
		return 16
	}
	return cx.st.FontSize
}

func (cx *cctx) rootFont() float32 {
	if cx.env.RootFont > 0 {
		return cx.env.RootFont
	}
	return 16
}

func (cx *cctx) cur() color.NRGBA { return cx.st.Color }

// inheritFrom starts a child style from its parent: inherited properties
// copied, the rest initial.
func inheritFrom(p *Style, env Env) Style {
	st := Initial(env.Fg)
	st.Visibility = p.Visibility
	st.Color = p.Color
	st.FontFamily = p.FontFamily
	st.FontSize = p.FontSize
	st.FontWeight = p.FontWeight
	st.Italic = p.Italic
	st.LineHeight = p.LineHeight
	st.LetterSpacing = p.LetterSpacing
	st.WordSpacing = p.WordSpacing
	st.TextAlign = p.TextAlign
	st.TextTransform = p.TextTransform
	st.WhiteSpace = p.WhiteSpace
	st.WordBreakAll = p.WordBreakAll
	st.wordBreak, st.overflowWrap = p.wordBreak, p.overflowWrap
	st.TextShadow = p.TextShadow
	st.Cursor = p.Cursor
	st.Pixelated = p.Pixelated
	st.PointerEvents = p.PointerEvents
	st.UserSelect = p.UserSelect
	st.Fill, st.FillNone = p.Fill, p.FillNone
	st.Stroke, st.StrokeNone = p.Stroke, p.StrokeNone
	st.StrokeWidth = p.StrokeWidth
	st.Vars = p.Vars
	st.BorderColor = [4]color.NRGBA{p.Color, p.Color, p.Color, p.Color}
	st.Outline.Color = p.Color
	return st
}

// Compute resolves a cascade into a computed style inheriting from parent
// (nil: the root, starting from Initial(env.Fg) with env.RootVars).
func Compute(c *Cascaded, parent *Style, env Env) Style {
	var st Style
	if parent == nil {
		st = Initial(env.Fg)
		st.Outline.Color = env.Fg
	} else {
		st = inheritFrom(parent, env)
	}
	if c == nil || len(c.Decls) == 0 {
		return st
	}
	cx := cctx{st: &st, parent: parent, env: env}
	cx.customs(c.Decls)
	// font-size first (em), then color (currentColor), then the rest
	for i := range c.Decls {
		if c.Decls[i].Prop == "font-size" {
			cx.one(&c.Decls[i])
		}
	}
	for i := range c.Decls {
		if c.Decls[i].Prop == "color" {
			cx.one(&c.Decls[i])
		}
	}
	for i := range c.Decls {
		d := &c.Decls[i]
		switch {
		case d.Prop == "font-size", d.Prop == "color", strings.HasPrefix(d.Prop, "--"),
			d.Prop == "text-wrap", d.Prop == "text-wrap-mode":
			continue
		}
		cx.one(d)
	}
	for i := range c.Decls {
		if p := c.Decls[i].Prop; p == "text-wrap" || p == "text-wrap-mode" {
			cx.one(&c.Decls[i])
		}
	}
	cx.finish()
	return st
}

// one computes a single winning declaration.
func (cx *cctx) one(d *Decl) {
	v := d.Value
	if d.Shorthand != "" || hasVar(v) {
		s, ok := cx.substitute(v)
		if !ok {
			cx.wide(d.Prop, "unset")
			return
		}
		v = s
		if d.Shorthand != "" {
			lv, ok := expandPending(d.Shorthand, d.Prop, v)
			if !ok {
				cx.wide(d.Prop, "unset")
				return
			}
			v = lv
		}
	}
	v = strings.TrimSpace(v)
	if kw := wideKeyword(v); kw != "" {
		cx.wide(d.Prop, kw)
		return
	}
	if !cx.apply(d.Prop, v) {
		// invalid at computed-value time: unset
		cx.wide(d.Prop, "unset")
	}
}

func wideKeyword(v string) string {
	if len(v) < 5 || len(v) > 12 {
		return ""
	}
	switch l := strings.ToLower(v); l {
	case "inherit", "initial", "unset", "revert", "revert-layer":
		return l
	}
	return ""
}

func hasVar(v string) bool {
	return strings.Contains(v, "var(") || strings.Contains(v, "env(") || strings.Contains(v, "VAR(") || strings.Contains(v, "Var(")
}

// --- custom properties and var() ---

func (cx *cctx) customs(ds Decls) {
	n := 0
	for i := range ds {
		if strings.HasPrefix(ds[i].Prop, "--") {
			n++
		}
	}
	if n == 0 {
		return
	}
	parentVars := cx.st.Vars
	vars := make(map[string]string, len(parentVars)+n)
	for k, v := range parentVars {
		vars[k] = v
	}
	own := make(map[string]string, n)
	for i := range ds {
		if strings.HasPrefix(ds[i].Prop, "--") {
			own[ds[i].Prop] = ds[i].Value
		}
	}
	state := make(map[string]uint8, n) // 1 visiting, 2 done
	var stack []string
	cyc := map[string]bool{}
	var resolve func(name string) (string, bool)
	parentLookup := func(name string) (string, bool) {
		if v, ok := parentVars[name]; ok {
			return v, v != invalidVar
		}
		return cx.rootVar(name, 0)
	}
	resolve = func(name string) (string, bool) {
		raw, mine := own[name]
		if !mine {
			return parentLookup(name)
		}
		switch state[name] {
		case 1:
			// a cycle: every property on it is invalid, fallbacks or not
			for i := len(stack) - 1; i >= 0; i-- {
				cyc[stack[i]] = true
				if stack[i] == name {
					break
				}
			}
			return "", false
		case 2:
			v := vars[name]
			return v, v != invalidVar
		}
		state[name] = 1
		stack = append(stack, name)
		val, ok := raw, true
		switch wideKeyword(strings.TrimSpace(raw)) {
		case "inherit", "unset", "revert", "revert-layer":
			val, ok = parentLookup(name)
		case "initial":
			ok = false
		default:
			if hasVar(raw) {
				val, ok = substVars(raw, resolve, 0)
			}
		}
		stack = stack[:len(stack)-1]
		if cyc[name] {
			ok = false
		}
		state[name] = 2
		if ok {
			vars[name] = val
		} else {
			vars[name] = invalidVar
		}
		return val, ok
	}
	for name := range own {
		resolve(name)
	}
	cx.st.Vars = vars
}

// rootVar looks a custom property up in env.RootVars, substituting any
// var() inside it against the root set.
func (cx *cctx) rootVar(name string, depth int) (string, bool) {
	v, ok := cx.env.RootVars[name]
	if !ok {
		return "", false
	}
	if depth > 24 {
		return "", false
	}
	if hasVar(v) {
		return substVars(v, func(n string) (string, bool) { return cx.rootVar(n, depth+1) }, depth+1)
	}
	return v, true
}

func (cx *cctx) lookupVar(name string) (string, bool) {
	if v, ok := cx.st.Vars[name]; ok {
		return v, v != invalidVar
	}
	return cx.rootVar(name, 0)
}

func (cx *cctx) substitute(v string) (string, bool) {
	return substVars(v, cx.lookupVar, 0)
}

// substVars replaces var(--x[, fallback]) and env(name[, fallback]) in v.
func substVars(v string, lookup func(string) (string, bool), depth int) (string, bool) {
	if depth > 32 {
		return "", false
	}
	toks := tokenize(v)
	var b strings.Builder
	last := 0
	changed := false
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.k != tFunction {
			continue
		}
		fn := strings.ToLower(t.s)
		if fn != "var" && fn != "env" {
			continue
		}
		// find the closing paren and the first top-level comma
		depthP := 0
		comma, end := -1, -1
		for j := i + 1; j < len(toks); j++ {
			switch toks[j].k {
			case tFunction, tLParen, tLBracket, tLBrace:
				depthP++
			case tRBracket, tRBrace:
				depthP--
			case tRParen:
				if depthP == 0 {
					end = j
				}
				depthP--
			case tComma:
				if depthP == 0 && comma < 0 {
					comma = j
				}
			}
			if end >= 0 {
				break
			}
		}
		closeAt := len(v)
		if end >= 0 {
			closeAt = toks[end].start
		} else {
			end = len(toks) - 1
		}
		nameEnd := closeAt
		if comma >= 0 {
			nameEnd = toks[comma].start
		}
		name := strings.TrimSpace(v[t.end:nameEnd])
		var rep string
		var ok bool
		if fn == "var" {
			if !strings.HasPrefix(name, "--") {
				return "", false
			}
			rep, ok = lookup(name)
		} else {
			switch strings.ToLower(name) {
			case "safe-area-inset-top", "safe-area-inset-right", "safe-area-inset-bottom", "safe-area-inset-left",
				"titlebar-area-x", "titlebar-area-y", "titlebar-area-height", "keyboard-inset-top",
				"keyboard-inset-bottom", "keyboard-inset-left", "keyboard-inset-right", "keyboard-inset-width", "keyboard-inset-height":
				rep, ok = "0px", true
			case "titlebar-area-width":
				rep, ok = "100vw", true
			}
		}
		if !ok {
			if comma < 0 {
				return "", false
			}
			fb := v[toks[comma].end:closeAt]
			if hasVar(fb) {
				fb, ok = substVars(fb, lookup, depth+1)
				if !ok {
					return "", false
				}
			}
			rep = fb
		}
		b.WriteString(v[last:t.start])
		b.WriteString(rep)
		last = closeAt + 1
		if last > len(v) {
			last = len(v)
		}
		changed = true
		i = end
	}
	if !changed {
		return v, true
	}
	b.WriteString(v[last:])
	return b.String(), true
}

// --- CSS-wide keywords ---

func (cx *cctx) wide(prop, kw string) {
	if cx.validate {
		return
	}
	inh := inheritedProps[prop] || strings.HasPrefix(prop, "--")
	useParent := kw == "inherit" || kw != "initial" && inh
	if useParent && cx.parent != nil {
		copyProp(prop, cx, cx.parent)
		return
	}
	in := Initial(cx.env.Fg)
	copyProp(prop, cx, &in)
}

// copyProp copies prop's computed value from src (inherit/initial).
func copyProp(prop string, cx *cctx, src *Style) {
	st := cx.st
	side := func(p string) int {
		switch {
		case strings.Contains(p, "top"):
			return 0
		case strings.Contains(p, "right"):
			return 1
		case strings.Contains(p, "bottom"):
			return 2
		}
		return 3
	}
	switch prop {
	case "display":
		st.Display = src.Display
	case "position":
		st.Position = src.Position
	case "visibility":
		st.Visibility = src.Visibility
	case "z-index":
		st.ZIndex, st.ZAuto = src.ZIndex, src.ZAuto
	case "top":
		st.Top = src.Top
	case "right":
		st.Right = src.Right
	case "bottom":
		st.Bottom = src.Bottom
	case "left":
		st.Left = src.Left
	case "width":
		st.Width = src.Width
	case "height":
		st.Height = src.Height
	case "min-width":
		st.MinWidth = src.MinWidth
	case "min-height":
		st.MinHeight = src.MinHeight
	case "max-width":
		st.MaxWidth = src.MaxWidth
	case "max-height":
		st.MaxHeight = src.MaxHeight
	case "box-sizing":
		st.BoxSizing = src.BoxSizing
	case "aspect-ratio":
		st.AspectRatio = src.AspectRatio
	case "margin-top", "margin-right", "margin-bottom", "margin-left":
		i := side(prop)
		st.Margin[i] = src.Margin[i]
	case "padding-top", "padding-right", "padding-bottom", "padding-left":
		i := side(prop)
		st.Padding[i] = src.Padding[i]
	case "border-top-width", "border-right-width", "border-bottom-width", "border-left-width":
		i := side(prop)
		st.BorderWidth[i] = src.BorderWidth[i]
	case "border-top-style", "border-right-style", "border-bottom-style", "border-left-style":
		i := side(prop)
		st.BorderStyle[i] = src.BorderStyle[i]
	case "border-top-color", "border-right-color", "border-bottom-color", "border-left-color":
		i := side(prop)
		st.BorderColor[i] = src.BorderColor[i]
		cx.bcSet[i] = src == cx.parent
	case "border-top-left-radius":
		st.Radius[0] = src.Radius[0]
	case "border-top-right-radius":
		st.Radius[1] = src.Radius[1]
	case "border-bottom-right-radius":
		st.Radius[2] = src.Radius[2]
	case "border-bottom-left-radius":
		st.Radius[3] = src.Radius[3]
	case "outline-width":
		st.Outline.Width = src.Outline.Width
	case "outline-style":
		st.Outline.Style = src.Outline.Style
	case "outline-color":
		st.Outline.Color = src.Outline.Color
		cx.ocSet = src == cx.parent
	case "outline-offset":
		st.Outline.Offset = src.Outline.Offset
	case "flex-direction":
		st.FlexDirection = src.FlexDirection
	case "flex-wrap":
		st.FlexWrap = src.FlexWrap
	case "justify-content":
		st.JustifyContent = src.JustifyContent
	case "align-items":
		st.AlignItems = src.AlignItems
	case "align-self":
		st.AlignSelf = src.AlignSelf
	case "align-content":
		st.AlignContent = src.AlignContent
	case "row-gap":
		st.RowGap = src.RowGap
	case "column-gap":
		st.ColGap = src.ColGap
	case "flex-grow":
		st.FlexGrow = src.FlexGrow
	case "flex-shrink":
		st.FlexShrink = src.FlexShrink
	case "flex-basis":
		st.FlexBasis = src.FlexBasis
	case "order":
		st.Order = src.Order
	case "grid-template-columns":
		st.GridColumns = src.GridColumns
		cx.gridSet = true
	case "overflow-x":
		st.OverflowX = src.OverflowX
	case "overflow-y":
		st.OverflowY = src.OverflowY
	case "opacity":
		st.Opacity = src.Opacity
	case "transform":
		cx.xform, cx.xformSet = src.Transform, true
	case "translate":
		cx.indiv[0], cx.indivSet[0] = nil, true
	case "rotate":
		cx.indiv[1], cx.indivSet[1] = nil, true
	case "scale":
		cx.indiv[2], cx.indivSet[2] = nil, true
	case "transform-origin":
		st.TransformOrigin = src.TransformOrigin
	case "filter":
		st.Filter = src.Filter
		cx.dropSh = nil
	case "backdrop-filter":
		st.BackdropBlur = src.BackdropBlur
	case "box-shadow":
		cx.boxSh, cx.boxShSet = src.BoxShadow, true
	case "background-color":
		st.Background.Color = src.Background.Color
	case "background-image":
		cx.bgImgs = []bgLayer{{url: src.Background.Image, grad: src.Background.Gradient}}
		cx.bgSet = true
	case "background-size":
		cx.bgSize = []string{src.Background.Size}
	case "background-position":
		cx.bgPos = []string{src.Background.Position}
	case "background-repeat":
		cx.bgRepeat = []bool{src.Background.Repeat}
	case "mix-blend-mode":
		st.MixBlend = src.MixBlend
	case "color":
		st.Color = src.Color
	case "font-family":
		st.FontFamily = src.FontFamily
	case "font-size":
		st.FontSize = src.FontSize
	case "font-weight":
		st.FontWeight = src.FontWeight
	case "font-style":
		st.Italic = src.Italic
	case "line-height":
		st.LineHeight = src.LineHeight
	case "letter-spacing":
		st.LetterSpacing = src.LetterSpacing
	case "word-spacing":
		st.WordSpacing = src.WordSpacing
	case "text-align":
		st.TextAlign = src.TextAlign
	case "text-decoration-line", "text-decoration-style":
		st.TextDecoration = src.TextDecoration
	case "text-decoration-color":
		st.DecoColor = src.DecoColor
	case "text-transform":
		st.TextTransform = src.TextTransform
	case "white-space":
		st.WhiteSpace = src.WhiteSpace
	case "text-overflow":
		st.TextOverflowEllipsis = src.TextOverflowEllipsis
	case "-webkit-line-clamp", "line-clamp":
		st.LineClamp = src.LineClamp
	case "word-break":
		st.wordBreak = src.wordBreak
	case "overflow-wrap":
		st.overflowWrap = src.overflowWrap
	case "vertical-align":
		st.VerticalAlign = src.VerticalAlign
	case "text-shadow":
		st.TextShadow = src.TextShadow
	case "cursor":
		st.Cursor = src.Cursor
	case "image-rendering":
		st.Pixelated = src.Pixelated
	case "pointer-events":
		st.PointerEvents = src.PointerEvents
	case "user-select":
		st.UserSelect = src.UserSelect
	case "content":
		st.Content, st.HasContent = src.Content, src.HasContent
	case "fill":
		st.Fill, st.FillNone = src.Fill, src.FillNone
	case "stroke":
		st.Stroke, st.StrokeNone = src.Stroke, src.StrokeNone
	case "stroke-width":
		st.StrokeWidth = src.StrokeWidth
	case "object-fit":
		st.ObjectFit = src.ObjectFit
	}
}

// inheritedProps are the inherited longhands (for unset/revert).
var inheritedProps = map[string]bool{
	"color": true, "font-family": true, "font-size": true, "font-weight": true, "font-style": true,
	"font-variant": true, "font-stretch": true, "line-height": true, "letter-spacing": true,
	"word-spacing": true, "text-align": true, "text-transform": true, "white-space": true,
	"word-break": true, "overflow-wrap": true, "text-shadow": true, "visibility": true,
	"cursor": true, "pointer-events": true, "user-select": true, "fill": true, "stroke": true,
	"stroke-width": true, "text-indent": true, "direction": true, "quotes": true,
	"list-style-type": true, "list-style-position": true, "list-style-image": true,
	"caret-color": true, "text-wrap": true, "text-wrap-mode": true, "tab-size": true,
	"hyphens": true, "font-variant-numeric": true, "font-feature-settings": true,
	"-webkit-font-smoothing": true, "text-rendering": true, "fill-opacity": true,
	"stroke-opacity": true, "color-scheme": true, "accent-color": true, "white-space-collapse": true,
	"line-break": true, "text-align-last": true, "font-kerning": true, "font-variation-settings": true,
	"font-optical-sizing": true, "-webkit-text-fill-color": true, "-webkit-text-stroke-color": true,
	"-webkit-text-stroke-width": true, "border-collapse": true, "border-spacing": true,
	"empty-cells": true, "caption-side": true, "orphans": true, "widows": true, "writing-mode": true,
	"text-underline-position": true, "text-underline-offset": true, "paint-order": true,
	"image-rendering": true, "text-size-adjust": true, "-webkit-text-size-adjust": true,
	"stroke-linecap": true, "stroke-linejoin": true, "stroke-dasharray": true, "stroke-dashoffset": true,
	"stroke-miterlimit": true, "fill-rule": true, "clip-rule": true, "font-synthesis": true,
	"text-emphasis-color": true, "text-emphasis-style": true, "text-emphasis-position": true,
	"hanging-punctuation": true, "font-size-adjust": true, "font-palette": true,
	"forced-color-adjust": true, "print-color-adjust": true, "-webkit-print-color-adjust": true,
	"-webkit-tap-highlight-color": true, "-webkit-touch-callout": true, "-webkit-user-modify": true,
	"text-wrap-style": true,
}

// --- finishing ---

func (cx *cctx) finish() {
	st := cx.st
	for i := 0; i < 4; i++ {
		if !cx.bcSet[i] {
			st.BorderColor[i] = st.Color
		}
		if st.BorderStyle[i] == BorderNone || st.BorderStyle[i] == BorderHidden {
			st.BorderWidth[i] = 0
		}
	}
	if !cx.ocSet {
		st.Outline.Color = st.Color
	}
	if st.Outline.Style == BorderNone || st.Outline.Style == BorderHidden {
		st.Outline.Width = 0
	}
	// background layers: draw the first layer that has an image
	if cx.bgSet || cx.bgSize != nil || cx.bgPos != nil || cx.bgRepeat != nil {
		idx := 0
		if cx.bgSet {
			st.Background.Image, st.Background.Gradient = "", nil
			for i, l := range cx.bgImgs {
				if l.url != "" || l.grad != nil {
					idx = i
					st.Background.Image, st.Background.Gradient = l.url, l.grad
					break
				}
			}
		}
		if len(cx.bgSize) > 0 {
			st.Background.Size = cx.bgSize[idx%len(cx.bgSize)]
		}
		if len(cx.bgPos) > 0 {
			st.Background.Position = cx.bgPos[idx%len(cx.bgPos)]
		}
		if len(cx.bgRepeat) > 0 {
			st.Background.Repeat = cx.bgRepeat[idx%len(cx.bgRepeat)]
		}
	}
	// transforms: translate, rotate, scale, then transform
	if cx.indiv != [3]*Transform{} || cx.xformSet {
		var list []Transform
		for _, t := range cx.indiv {
			if t != nil {
				list = append(list, *t)
			}
		}
		list = append(list, cx.xform...)
		st.Transform = list
	}
	if cx.boxShSet || cx.dropSh != nil {
		var sh []Shadow
		sh = append(sh, cx.boxSh...)
		sh = append(sh, cx.dropSh...)
		st.BoxShadow = sh
	}
	if cx.webkit {
		st.FlexDirection = Column
		if cx.orient == 'h' {
			st.FlexDirection = Row
		}
		if cx.rev {
			st.FlexDirection++
			if st.FlexDirection == Column {
				st.FlexDirection = RowReverse
			}
		}
	}
	if !cx.gridSet && cx.areas > 0 && st.GridColumns == 0 {
		st.GridColumns = cx.areas
	}
	switch cx.wrapMode {
	case 1:
		switch st.WhiteSpace {
		case WSNormal, WSPreLine:
			st.WhiteSpace = WSNoWrap
		case WSPreWrap, WSBreakSpaces:
			st.WhiteSpace = WSPre
		}
	case -1:
		switch st.WhiteSpace {
		case WSNoWrap:
			st.WhiteSpace = WSNormal
		case WSPre:
			st.WhiteSpace = WSPreWrap
		}
	}
	st.WordBreakAll = st.wordBreak == 1 || st.overflowWrap != 0
}

// --- per-property ---

func kw(cs []cv) string {
	if len(cs) == 1 && cs[0].t.k == tIdent {
		return strings.ToLower(cs[0].t.s)
	}
	return ""
}

// apply computes one longhand from its (substituted) value; false means
// the value is invalid for the property.
func (cx *cctx) apply(prop, v string) bool {
	if v == "" {
		return false
	}
	cs := parseCVs(v)
	if len(cs) == 0 {
		return false
	}
	st := cx.st
	k := kw(cs)
	one := len(cs) == 1
	switch prop {
	case "display":
		return cx.display(cs)
	case "position":
		switch k {
		case "static":
			st.Position = PosStatic
		case "relative":
			st.Position = PosRelative
		case "absolute":
			st.Position = PosAbsolute
		case "fixed":
			st.Position = PosFixed
		case "sticky", "-webkit-sticky":
			st.Position = PosSticky
		default:
			return false
		}
	case "visibility":
		switch k {
		case "visible":
			st.Visibility = Visible
		case "hidden":
			st.Visibility = Hidden
		case "collapse":
			st.Visibility = Collapse
		default:
			return false
		}
	case "z-index":
		if k == "auto" {
			st.ZAuto, st.ZIndex = true, 0
			return true
		}
		n, ok := cx.integer(&cs[0])
		if !one || !ok {
			return false
		}
		st.ZAuto, st.ZIndex = false, int32(n)
	case "top", "right", "bottom", "left":
		l, ok := cx.lengthKw(cs, true, true, false)
		if !ok {
			return false
		}
		switch prop {
		case "top":
			st.Top = l
		case "right":
			st.Right = l
		case "bottom":
			st.Bottom = l
		default:
			st.Left = l
		}
	case "width", "height", "min-width", "min-height":
		l, ok := cx.size(cs, true, false)
		if !ok {
			return false
		}
		switch prop {
		case "width":
			st.Width = l
		case "height":
			st.Height = l
		case "min-width":
			st.MinWidth = l
		default:
			st.MinHeight = l
		}
	case "max-width", "max-height":
		l, ok := cx.size(cs, false, true)
		if !ok {
			return false
		}
		if prop == "max-width" {
			st.MaxWidth = l
		} else {
			st.MaxHeight = l
		}
	case "box-sizing":
		switch k {
		case "content-box":
			st.BoxSizing = ContentBox
		case "border-box":
			st.BoxSizing = BorderBox
		default:
			return false
		}
	case "aspect-ratio":
		return cx.aspect(cs)
	case "margin-top", "margin-right", "margin-bottom", "margin-left",
		"padding-top", "padding-right", "padding-bottom", "padding-left":
		margin := prop[0] == 'm'
		l, ok := cx.lengthKw(cs, margin, true, !margin)
		if !ok {
			return false
		}
		i := sideIndex(prop)
		if margin {
			st.Margin[i] = l
		} else {
			st.Padding[i] = l
		}
	case "border-top-width", "border-right-width", "border-bottom-width", "border-left-width":
		w, ok := cx.lineWidth(cs)
		if !ok {
			return false
		}
		st.BorderWidth[sideIndex(prop)] = w
	case "border-top-style", "border-right-style", "border-bottom-style", "border-left-style":
		s, ok := borderStyle(k, false)
		if !ok {
			return false
		}
		st.BorderStyle[sideIndex(prop)] = s
	case "border-top-color", "border-right-color", "border-bottom-color", "border-left-color":
		c, ok := cx.color(&cs[0])
		if !one || !ok {
			return false
		}
		i := sideIndex(prop)
		st.BorderColor[i] = c
		cx.bcSet[i] = true
	case "border-top-left-radius", "border-top-right-radius", "border-bottom-right-radius", "border-bottom-left-radius":
		if len(cs) > 2 {
			return false
		}
		var l Length
		for j := range cs {
			x, ok := cx.length(&cs[j], true)
			if !ok || negLen(&cs[j], x) {
				return false
			}
			if j == 0 {
				l = x
			}
		}
		switch prop {
		case "border-top-left-radius":
			st.Radius[0] = l
		case "border-top-right-radius":
			st.Radius[1] = l
		case "border-bottom-right-radius":
			st.Radius[2] = l
		default:
			st.Radius[3] = l
		}
	case "outline-width":
		w, ok := cx.lineWidth(cs)
		if !ok {
			return false
		}
		st.Outline.Width = w
	case "outline-style":
		s, ok := borderStyle(k, true)
		if !ok {
			return false
		}
		st.Outline.Style = s
	case "outline-color":
		if k == "invert" || k == "auto" {
			cx.ocSet = false
			return true
		}
		c, ok := cx.color(&cs[0])
		if !one || !ok {
			return false
		}
		st.Outline.Color, cx.ocSet = c, true
	case "outline-offset":
		p, ok := cx.px(&cs[0])
		if !one || !ok {
			return false
		}
		st.Outline.Offset = p
	case "flex-direction":
		switch k {
		case "row":
			st.FlexDirection = Row
		case "row-reverse":
			st.FlexDirection = RowReverse
		case "column":
			st.FlexDirection = Column
		case "column-reverse":
			st.FlexDirection = ColumnReverse
		default:
			return false
		}
	case "flex-wrap":
		switch k {
		case "nowrap":
			st.FlexWrap = NoWrap
		case "wrap":
			st.FlexWrap = Wrap
		case "wrap-reverse":
			st.FlexWrap = WrapReverse
		default:
			return false
		}
	case "justify-content", "align-items", "align-self", "align-content":
		a, ok := parseAlign(cs, prop)
		if !ok {
			return false
		}
		switch prop {
		case "justify-content":
			st.JustifyContent = a
		case "align-items":
			st.AlignItems = a
		case "align-self":
			st.AlignSelf = a
		default:
			st.AlignContent = a
		}
	case "justify-items", "justify-self":
		_, ok := parseAlign(cs, prop)
		return ok || k == "legacy" || k == "left" || k == "right"
	case "row-gap", "column-gap":
		var l Length
		if k == "normal" {
			l = Zero
		} else {
			x, ok := cx.length(&cs[0], true)
			if !one || !ok || negLen(&cs[0], x) {
				return false
			}
			l = x
		}
		if prop == "row-gap" {
			st.RowGap = l
		} else {
			st.ColGap = l
		}
	case "flex-grow", "flex-shrink":
		n, ok := cx.number(&cs[0])
		if !one || !ok || n < 0 {
			return false
		}
		if prop == "flex-grow" {
			st.FlexGrow = float32(n)
		} else {
			st.FlexShrink = float32(n)
		}
	case "flex-basis":
		switch k {
		case "auto", "content":
			st.FlexBasis = Auto
			return true
		}
		l, ok := cx.size(cs, true, false)
		if !ok {
			return false
		}
		st.FlexBasis = l
	case "order":
		n, ok := cx.integer(&cs[0])
		if !one || !ok {
			return false
		}
		st.Order = int32(n)
	case "-webkit-box-orient":
		switch k {
		case "horizontal", "inline-axis":
			cx.orient = 'h'
		case "vertical", "block-axis":
			cx.orient = 'v'
		default:
			return false
		}
	case "-webkit-box-direction":
		switch k {
		case "normal":
			cx.rev = false
		case "reverse":
			cx.rev = true
		default:
			return false
		}
	case "-webkit-box-pack":
		m := map[string]Align{"start": AlignStart, "end": AlignEnd, "center": AlignCenter, "justify": AlignSpaceBetween}
		a, ok := m[k]
		if !ok {
			return false
		}
		st.JustifyContent = a
	case "-webkit-box-align":
		m := map[string]Align{"start": AlignStart, "end": AlignEnd, "center": AlignCenter, "baseline": AlignBaseline, "stretch": AlignStretch}
		a, ok := m[k]
		if !ok {
			return false
		}
		st.AlignItems = a
	case "-webkit-box-flex":
		n, ok := cx.number(&cs[0])
		if !one || !ok {
			return false
		}
		st.FlexGrow = float32(n)
	case "grid-template-columns":
		n, ok := gridTracks(cs)
		if !ok {
			return false
		}
		st.GridColumns, cx.gridSet = n, true
	case "grid-template-areas":
		if k == "none" {
			cx.areas = 0
			return true
		}
		if cs[0].t.k != tString {
			return false
		}
		cx.areas = len(strings.Fields(cs[0].t.s))
	case "overflow-x", "overflow-y":
		var o Overflow
		switch k {
		case "visible":
			o = OverflowVisible
		case "hidden":
			o = OverflowHidden
		case "scroll":
			o = OverflowScroll
		case "auto", "overlay":
			o = OverflowAuto
		case "clip":
			o = OverflowClip
		default:
			return false
		}
		if prop == "overflow-x" {
			st.OverflowX = o
		} else {
			st.OverflowY = o
		}
	case "opacity":
		n, ok := cx.numPct(&cs[0])
		if !one || !ok {
			return false
		}
		st.Opacity = float32(clamp01(n))
	case "transform":
		if k == "none" {
			cx.xform, cx.xformSet = nil, true
			return true
		}
		list, ok := cx.transforms(cs)
		if !ok {
			return false
		}
		cx.xform, cx.xformSet = list, true
	case "translate", "rotate", "scale":
		return cx.indivTransform(prop, cs, k)
	case "transform-origin":
		o, ok := cx.origin(cs)
		if !ok {
			return false
		}
		st.TransformOrigin = o
	case "filter":
		if k == "none" {
			st.Filter, cx.dropSh = Filter{}, nil
			return true
		}
		return cx.filter(cs)
	case "backdrop-filter":
		if k == "none" {
			st.BackdropBlur = 0
			return true
		}
		var blur float32
		for i := range cs {
			c := &cs[i]
			if c.t.k == tURL || c.t.k == tFunction && c.name == "url" {
				continue
			}
			if c.t.k != tFunction {
				return false
			}
			if c.name == "blur" {
				if len(c.args) == 0 {
					continue
				}
				p, ok := cx.px(&c.args[0])
				if !ok || p < 0 {
					return false
				}
				blur += p
			} else if !filterFns[c.name] {
				return false
			}
		}
		st.BackdropBlur = blur
	case "box-shadow":
		if k == "none" {
			cx.boxSh, cx.boxShSet = nil, true
			return true
		}
		sh, ok := cx.shadows(cs, true)
		if !ok {
			return false
		}
		cx.boxSh, cx.boxShSet = sh, true
	case "text-shadow":
		if k == "none" {
			st.TextShadow = nil
			return true
		}
		sh, ok := cx.shadows(cs, false)
		if !ok {
			return false
		}
		st.TextShadow = sh
	case "background-color":
		c, ok := cx.color(&cs[0])
		if !one || !ok {
			return false
		}
		st.Background.Color = c
	case "background-image":
		var layers []bgLayer
		for _, part := range splitComma(cs) {
			if len(part) != 1 {
				return false
			}
			l, ok := cx.image(&part[0])
			if !ok {
				return false
			}
			layers = append(layers, l)
		}
		cx.bgImgs, cx.bgSet = layers, true
	case "background-size":
		var out []string
		for _, part := range splitComma(cs) {
			s, ok := cx.bgSizeText(v, part)
			if !ok {
				return false
			}
			out = append(out, s)
		}
		cx.bgSize = out
	case "background-position":
		var out []string
		for _, part := range splitComma(cs) {
			if !cx.validPosition(part) {
				return false
			}
			out = append(out, strings.ToLower(spanText(v, part)))
		}
		cx.bgPos = out
	case "background-repeat":
		var out []bool
		for _, part := range splitComma(cs) {
			r, ok := bgRepeat(part)
			if !ok {
				return false
			}
			out = append(out, r)
		}
		cx.bgRepeat = out
	case "mix-blend-mode":
		if !blendModes[k] {
			return false
		}
		st.MixBlend = k
	case "color":
		c, ok := cx.color(&cs[0])
		if !one || !ok {
			return false
		}
		st.Color = c
	case "font-family":
		f, ok := fontFamily(cs, v)
		if !ok {
			return false
		}
		st.FontFamily = f
	case "font-size":
		cx.fontSizing = true
		defer func() { cx.fontSizing = false }()
		return cx.fontSize(cs, k)
	case "font-weight":
		w, ok := cx.fontWeight(cs, k)
		if !ok {
			return false
		}
		st.FontWeight = w
	case "font-style":
		switch k {
		case "normal":
			st.Italic = false
		case "italic":
			st.Italic = true
		default:
			if len(cs) >= 1 && cs[0].t.ident("oblique") {
				st.Italic = true
				return true
			}
			return false
		}
	case "line-height":
		switch {
		case k == "normal":
			st.LineHeight = Auto
		case cs[0].t.k == tNumber:
			if !one || cs[0].t.num < 0 {
				return false
			}
			st.LineHeight = Length{Pct: float32(cs[0].t.num * 100)}
		case cs[0].t.k == tPercentage:
			st.LineHeight = Px(float32(cs[0].t.num) / 100 * st.FontSize)
		default:
			if !one {
				return false
			}
			if n, ok := cx.number(&cs[0]); ok {
				st.LineHeight = Length{Pct: float32(n * 100)}
				return true
			}
			l, ok := cx.length(&cs[0], true)
			if !ok {
				return false
			}
			st.LineHeight = Px(l.Resolve(st.FontSize, cx.env.Width, cx.env.Height, 0))
		}
	case "letter-spacing", "word-spacing":
		var p float32
		if k != "normal" {
			l, ok := cx.length(&cs[0], true)
			if !one || !ok {
				return false
			}
			p = l.Resolve(st.FontSize, cx.env.Width, cx.env.Height, 0)
		}
		if prop == "letter-spacing" {
			st.LetterSpacing = p
		} else {
			st.WordSpacing = p
		}
	case "text-align":
		switch k {
		case "start":
			st.TextAlign = TextStart
		case "end":
			st.TextAlign = TextEnd
		case "left", "-webkit-left":
			st.TextAlign = TextLeft
		case "right", "-webkit-right":
			st.TextAlign = TextRight
		case "center", "-webkit-center", "-moz-center":
			st.TextAlign = TextCenter
		case "justify":
			st.TextAlign = TextJustify
		case "match-parent":
			if cx.parent != nil {
				st.TextAlign = cx.parent.TextAlign
			}
		default:
			return false
		}
	case "text-decoration-style":
		switch k {
		case "wavy":
			st.TextDecoration |= DecoWavy
		case "solid", "double", "dotted", "dashed":
			st.TextDecoration &^= DecoWavy
		default:
			return false
		}
	case "text-decoration-line":
		if k == "none" {
			st.TextDecoration &^= DecoLines
			return true
		}
		var d TextDecoration
		for i := range cs {
			switch strings.ToLower(cs[i].t.s) {
			case "underline":
				d |= DecoUnderline
			case "line-through":
				d |= DecoLineThrough
			case "overline":
				d |= DecoOverline
			case "blink", "spelling-error", "grammar-error":
			default:
				return false
			}
			if cs[i].t.k != tIdent {
				return false
			}
		}
		st.TextDecoration = d | st.TextDecoration&^DecoLines
	case "text-decoration-color":
		if k == "currentcolor" {
			st.DecoColor = color.NRGBA{}
			return true
		}
		c, ok := cx.color(&cs[0])
		if !one || !ok {
			return false
		}
		st.DecoColor = c
	case "text-transform":
		switch k {
		case "none", "full-width", "full-size-kana", "math-auto":
			st.TextTransform = TTNone
		case "uppercase":
			st.TextTransform = TTUppercase
		case "lowercase":
			st.TextTransform = TTLowercase
		case "capitalize":
			st.TextTransform = TTCapitalize
		default:
			return false
		}
	case "white-space":
		switch strings.ToLower(v) {
		case "normal":
			st.WhiteSpace = WSNormal
		case "nowrap", "collapse nowrap", "nowrap collapse":
			st.WhiteSpace = WSNoWrap
		case "pre", "preserve nowrap", "nowrap preserve":
			st.WhiteSpace = WSPre
		case "pre-wrap", "preserve", "preserve wrap", "wrap preserve":
			st.WhiteSpace = WSPreWrap
		case "pre-line", "preserve-breaks", "preserve-breaks wrap":
			st.WhiteSpace = WSPreLine
		case "break-spaces":
			st.WhiteSpace = WSBreakSpaces
		case "-webkit-nowrap":
			st.WhiteSpace = WSNoWrap
		default:
			return false
		}
	case "text-wrap", "text-wrap-mode":
		for i := range cs {
			switch strings.ToLower(cs[i].t.s) {
			case "nowrap":
				cx.wrapMode = 1
			case "wrap":
				cx.wrapMode = -1
			case "balance", "pretty", "stable", "auto":
				if prop == "text-wrap-mode" {
					return false
				}
			default:
				return false
			}
		}
	case "text-overflow":
		if len(cs) > 2 {
			return false
		}
		last := &cs[len(cs)-1]
		for i := range cs {
			if cs[i].t.k != tIdent && cs[i].t.k != tString {
				return false
			}
		}
		st.TextOverflowEllipsis = last.t.ident("ellipsis") || last.t.k == tString
	case "-webkit-line-clamp", "line-clamp":
		if k == "none" {
			st.LineClamp = 0
			return true
		}
		n, ok := cx.integer(&cs[0])
		if !ok || n < 1 {
			return false
		}
		st.LineClamp = n
	case "word-break":
		switch k {
		case "normal", "keep-all", "auto-phrase", "manual":
			st.wordBreak = 0
		case "break-all":
			st.wordBreak = 1
		case "break-word":
			st.wordBreak = 2
		default:
			return false
		}
		if st.wordBreak == 2 {
			st.overflowWrap = 1
		}
	case "overflow-wrap":
		switch k {
		case "normal":
			st.overflowWrap = 0
		case "break-word", "anywhere":
			st.overflowWrap = 1
		default:
			return false
		}
	case "vertical-align":
		switch k {
		case "baseline":
			st.VerticalAlign = VABaseline
		case "middle", "center":
			st.VerticalAlign = VAMiddle
		case "top":
			st.VerticalAlign = VATop
		case "bottom":
			st.VerticalAlign = VABottom
		case "text-top":
			st.VerticalAlign = VATextTop
		case "text-bottom":
			st.VerticalAlign = VATextBottom
		case "sub":
			st.VerticalAlign = VASub
		case "super":
			st.VerticalAlign = VASuper
		default:
			if _, ok := cx.length(&cs[0], true); !one || !ok {
				return false
			}
			st.VerticalAlign = VABaseline
		}
	case "cursor":
		return cx.cursor(cs)
	case "image-rendering":
		switch k {
		case "pixelated", "crisp-edges", "-moz-crisp-edges", "-webkit-optimize-contrast", "optimizespeed":
			st.Pixelated = true
		case "auto", "smooth", "high-quality", "optimizequality":
			st.Pixelated = false
		default:
			return false
		}
	case "pointer-events":
		switch k {
		case "none":
			st.PointerEvents = false
		case "auto", "all", "visiblepainted", "visiblefill", "visiblestroke", "visible",
			"painted", "fill", "stroke", "bounding-box":
			st.PointerEvents = true
		default:
			return false
		}
	case "user-select":
		switch k {
		case "none":
			st.UserSelect = false
		case "auto":
			if cx.parent != nil {
				st.UserSelect = cx.parent.UserSelect
			} else {
				st.UserSelect = true
			}
		case "text", "all", "contain", "element":
			st.UserSelect = true
		default:
			return false
		}
	case "content":
		return cx.content(cs, k)
	case "fill", "stroke":
		c, none, ok := cx.paint(cs, k)
		if !ok {
			return false
		}
		if prop == "fill" {
			st.Fill, st.FillNone = c, none
		} else {
			st.Stroke, st.StrokeNone = c, none
		}
	case "stroke-width":
		cx.quirkUnitless = true
		l, ok := cx.length(&cs[0], true)
		cx.quirkUnitless = false
		if !one || !ok {
			return false
		}
		st.StrokeWidth = l.Resolve(0, cx.env.Width, cx.env.Height, 0)
	case "object-fit":
		switch k {
		case "fill":
			st.ObjectFit = FitFill
		case "contain":
			st.ObjectFit = FitContain
		case "cover":
			st.ObjectFit = FitCover
		case "none":
			st.ObjectFit = FitNone
		case "scale-down":
			st.ObjectFit = FitScaleDown
		default:
			return false
		}
	default:
		return acceptedProps[prop] || inheritedProps[prop]
	}
	return true
}

// negLen reports a negative plain length. Math functions are never
// rejected for their sign: browsers clamp them at used-value time.
func negLen(c *cv, l Length) bool {
	if c.t.k == tFunction {
		return false
	}
	return l.Px < 0 || l.Pct < 0 || l.Vw < 0 || l.Vh < 0
}

func sideIndex(p string) int {
	switch {
	case strings.Contains(p, "-top"):
		return 0
	case strings.Contains(p, "-right"):
		return 1
	case strings.Contains(p, "-bottom"):
		return 2
	}
	return 3
}

// lengthKw parses a length-percentage with optional auto; nonNeg rejects
// negative values.
func (cx *cctx) lengthKw(cs []cv, auto, pct, nonNeg bool) (Length, bool) {
	if len(cs) != 1 {
		return Length{}, false
	}
	if auto && cs[0].t.ident("auto") {
		return Auto, true
	}
	l, ok := cx.length(&cs[0], pct)
	if !ok || nonNeg && negLen(&cs[0], l) {
		return Length{}, false
	}
	return l, true
}

// size parses width/height-like values.
func (cx *cctx) size(cs []cv, auto, none bool) (Length, bool) {
	if len(cs) != 1 {
		return Length{}, false
	}
	c := &cs[0]
	if c.t.k == tIdent {
		switch strings.ToLower(c.t.s) {
		case "auto":
			if auto {
				return Auto, true
			}
		case "none":
			if none {
				return None, true
			}
		case "min-content", "-webkit-min-content", "-moz-min-content":
			return Length{Kind: LenMinContent}, true
		case "max-content", "-webkit-max-content", "-moz-max-content":
			return Length{Kind: LenMaxContent}, true
		case "fit-content", "-webkit-fit-content", "-moz-fit-content":
			return Length{Kind: LenFitContent}, true
		case "stretch", "-webkit-fill-available", "-moz-available", "fill-available":
			return Pct(100), true
		}
		return Length{}, false
	}
	if c.isFn("fit-content") {
		return Length{Kind: LenFitContent}, len(c.args) == 1
	}
	l, ok := cx.length(c, true)
	if !ok || negLen(c, l) {
		return Length{}, false
	}
	return l, true
}

func (cx *cctx) aspect(cs []cv) bool {
	var nums []float64
	for i := range cs {
		c := &cs[i]
		switch {
		case c.t.ident("auto"):
		case c.t.is('/'):
		default:
			n, ok := cx.number(c)
			if !ok || n < 0 {
				return false
			}
			nums = append(nums, n)
		}
	}
	switch len(nums) {
	case 0:
		if kw(cs) != "auto" {
			return false
		}
		cx.st.AspectRatio = 0
	case 1:
		cx.st.AspectRatio = float32(nums[0])
	case 2:
		if nums[1] == 0 {
			cx.st.AspectRatio = 0
		} else {
			cx.st.AspectRatio = float32(nums[0] / nums[1])
		}
	default:
		return false
	}
	return true
}

func (cx *cctx) lineWidth(cs []cv) (float32, bool) {
	if len(cs) != 1 {
		return 0, false
	}
	switch kw(cs) {
	case "thin":
		return 1, true
	case "medium":
		return 3, true
	case "thick":
		return 5, true
	}
	p, ok := cx.px(&cs[0])
	if !ok || p < 0 {
		return 0, false
	}
	return p, true
}

func borderStyle(k string, outline bool) (BorderStyle, bool) {
	switch k {
	case "none":
		return BorderNone, true
	case "hidden":
		if outline {
			return 0, false
		}
		return BorderHidden, true
	case "solid", "groove", "ridge", "inset", "outset":
		return BorderSolid, true
	case "dashed":
		return BorderDashed, true
	case "dotted":
		return BorderDotted, true
	case "double":
		return BorderDouble, true
	case "auto":
		if outline {
			return BorderSolid, true
		}
	}
	return 0, false
}

func parseAlign(cs []cv, prop string) (Align, bool) {
	// drop overflow-position / baseline-position prefixes
	for len(cs) > 1 {
		switch strings.ToLower(cs[0].t.s) {
		case "safe", "unsafe", "first", "last":
			if cs[0].t.k == tIdent {
				cs = cs[1:]
				continue
			}
		}
		break
	}
	if len(cs) != 1 || cs[0].t.k != tIdent {
		return 0, false
	}
	switch strings.ToLower(cs[0].t.s) {
	case "normal":
		return AlignAuto, true
	case "auto":
		if prop == "align-self" || prop == "justify-self" {
			return AlignAuto, true
		}
	case "start", "flex-start", "self-start":
		return AlignStart, true
	case "left":
		if prop == "justify-content" || prop == "justify-items" || prop == "justify-self" {
			return AlignStart, true
		}
	case "end", "flex-end", "self-end":
		return AlignEnd, true
	case "right":
		if prop == "justify-content" || prop == "justify-items" || prop == "justify-self" {
			return AlignEnd, true
		}
	case "center":
		return AlignCenter, true
	case "stretch":
		return AlignStretch, true
	case "baseline":
		return AlignBaseline, true
	case "space-between":
		if prop == "justify-content" || prop == "align-content" {
			return AlignSpaceBetween, true
		}
	case "space-around":
		if prop == "justify-content" || prop == "align-content" {
			return AlignSpaceAround, true
		}
	case "space-evenly":
		if prop == "justify-content" || prop == "align-content" {
			return AlignSpaceEvenly, true
		}
	}
	return 0, false
}

func (cx *cctx) display(cs []cv) bool {
	st := cx.st
	words := make([]string, 0, 2)
	for i := range cs {
		if cs[i].t.k != tIdent {
			return false
		}
		words = append(words, strings.ToLower(cs[i].t.s))
	}
	cx.webkit = false
	switch strings.Join(words, " ") {
	case "none":
		st.Display = DisplayNone
	case "block", "block flow", "flow-root", "block flow-root", "flow", "table-caption", "ruby-text", "table-row-group", "table-header-group", "table-footer-group":
		st.Display = DisplayBlock
	case "inline", "inline flow", "ruby", "ruby-base", "ruby-text-container", "ruby-base-container":
		st.Display = DisplayInline
	case "inline-block", "inline flow-root":
		st.Display = DisplayInlineBlock
	case "flex", "block flex":
		st.Display = DisplayFlex
	case "inline-flex", "inline flex":
		st.Display = DisplayInlineFlex
	case "grid", "block grid":
		st.Display = DisplayGrid
	case "inline-grid", "inline grid":
		st.Display = DisplayInlineGrid
	case "contents":
		st.Display = DisplayContents
	case "list-item", "block list-item", "list-item block", "flow list-item", "list-item flow":
		st.Display = DisplayListItem
	case "inline list-item":
		st.Display = DisplayInlineBlock
	case "table", "inline-table", "block table", "inline table":
		st.Display = DisplayTable
	case "table-row":
		st.Display = DisplayTableRow
	case "table-cell":
		st.Display = DisplayTableCell
	case "table-column", "table-column-group":
		st.Display = DisplayNone
	case "-webkit-box", "-moz-box", "-ms-flexbox", "-webkit-flex":
		st.Display = DisplayFlex
		cx.webkit = words[0] == "-webkit-box" || words[0] == "-moz-box"
	case "-webkit-inline-box", "-moz-inline-box", "-ms-inline-flexbox", "-webkit-inline-flex":
		st.Display = DisplayInlineFlex
		cx.webkit = words[0] == "-webkit-inline-box" || words[0] == "-moz-inline-box"
	default:
		return false
	}
	return true
}

func gridTracks(cs []cv) (int, bool) {
	if k := kw(cs); k == "none" || k == "subgrid" || k == "masonry" {
		return 0, true
	}
	n := 0
	for i := range cs {
		c := &cs[i]
		switch {
		case c.t.k == tLBracket:
			continue // line names
		case c.isFn("repeat"):
			parts := splitComma(c.args)
			if len(parts) != 2 || len(parts[0]) != 1 {
				return 0, false
			}
			inner, ok := gridTracks(parts[1])
			if !ok {
				return 0, false
			}
			r := parts[0][0]
			if r.t.k == tIdent {
				return 0, true // auto-fill / auto-fit: auto-flow
			}
			if r.t.k != tNumber || !r.t.isInt || r.t.num < 1 {
				return 0, false
			}
			n += int(r.t.num) * inner
		case c.t.k == tDimension, c.t.k == tPercentage, c.t.k == tNumber && c.t.num == 0:
			n++
		case c.t.k == tIdent:
			switch strings.ToLower(c.t.s) {
			case "auto", "min-content", "max-content":
				n++
			default:
				return 0, false
			}
		case c.t.k == tFunction:
			switch c.name {
			case "minmax", "fit-content", "calc", "min", "max", "clamp":
				n++
			default:
				return 0, false
			}
		default:
			return 0, false
		}
	}
	return n, n > 0
}

// --- transforms ---

func (cx *cctx) transforms(cs []cv) ([]Transform, bool) {
	var out []Transform
	for i := range cs {
		c := &cs[i]
		if c.t.k != tFunction {
			return nil, false
		}
		args := splitComma(c.args)
		var flat []cv
		for _, a := range args {
			if len(a) != 1 {
				return nil, false
			}
			flat = append(flat, a[0])
		}
		if len(c.args) == 0 {
			return nil, false
		}
		lenArg := func(j int) (Length, bool) {
			if j >= len(flat) {
				return Zero, true
			}
			return cx.length(&flat[j], true)
		}
		numArg := func(j int, def float64) (float64, bool) {
			if j >= len(flat) {
				return def, true
			}
			return cx.numPct(&flat[j])
		}
		angArg := func(j int) (float64, bool) {
			if j >= len(flat) {
				return 0, true
			}
			return cx.angle(&flat[j], false)
		}
		switch c.name {
		case "translate", "translate3d", "translatex", "translatey", "translatez":
			var x, y Length
			var ok1, ok2 = true, true
			switch c.name {
			case "translatex":
				x, ok1 = lenArg(0)
			case "translatey":
				y, ok2 = lenArg(0)
			case "translatez":
				_, ok1 = cx.length(&flat[0], false)
			default:
				if len(flat) > 3 || c.name == "translate" && len(flat) > 2 {
					return nil, false
				}
				x, ok1 = lenArg(0)
				y, ok2 = lenArg(1)
			}
			if !ok1 || !ok2 {
				return nil, false
			}
			if c.name != "translatez" {
				out = append(out, Transform{Kind: TTranslate, X: x, Y: y})
			}
		case "scale", "scale3d", "scalex", "scaley", "scalez":
			sx, ok1 := numArg(0, 1)
			sy, ok2 := numArg(1, sx)
			if !ok1 || !ok2 {
				return nil, false
			}
			switch c.name {
			case "scalex":
				sy = 1
			case "scaley":
				sx, sy = 1, sx
			case "scalez":
				continue
			case "scale3d":
				if len(flat) != 3 {
					return nil, false
				}
			}
			out = append(out, Transform{Kind: TScale, SX: float32(sx), SY: float32(sy)})
		case "rotate", "rotatez":
			a, ok := angArg(0)
			if !ok || len(flat) != 1 {
				return nil, false
			}
			out = append(out, Transform{Kind: TRotate, Angle: float32(a)})
		case "rotatex", "rotatey":
			a, ok := angArg(0)
			if !ok || len(flat) != 1 {
				return nil, false
			}
			f := float32(math.Cos(a))
			if c.name == "rotatex" {
				out = append(out, Transform{Kind: TScale, SX: 1, SY: f})
			} else {
				out = append(out, Transform{Kind: TScale, SX: f, SY: 1})
			}
		case "rotate3d":
			if len(flat) != 4 {
				return nil, false
			}
			var v [3]float64
			for j := 0; j < 3; j++ {
				n, ok := cx.number(&flat[j])
				if !ok {
					return nil, false
				}
				v[j] = n
			}
			a, ok := angArg(3)
			if !ok {
				return nil, false
			}
			if v[0] == 0 && v[1] == 0 && v[2] != 0 {
				if v[2] < 0 {
					a = -a
				}
				out = append(out, Transform{Kind: TRotate, Angle: float32(a)})
			}
		case "skew", "skewx", "skewy":
			ax, ok1 := angArg(0)
			ay, ok2 := angArg(1)
			if !ok1 || !ok2 || len(flat) > 2 || c.name != "skew" && len(flat) != 1 {
				return nil, false
			}
			if c.name == "skewy" {
				ax, ay = 0, ax
			}
			out = append(out, Transform{Kind: TSkew, SX: float32(ax), SY: float32(ay)})
		case "matrix", "matrix3d":
			want := 6
			if c.name == "matrix3d" {
				want = 16
			}
			if len(flat) != want {
				return nil, false
			}
			var m []float32
			for j := range flat {
				n, ok := cx.number(&flat[j])
				if !ok {
					return nil, false
				}
				m = append(m, float32(n))
			}
			t := Transform{Kind: TMatrix}
			if want == 6 {
				copy(t.M[:], m)
			} else {
				t.M = [6]float32{m[0], m[1], m[4], m[5], m[12], m[13]}
			}
			out = append(out, t)
		case "perspective":
			// no 2D effect
		default:
			return nil, false
		}
	}
	return out, true
}

func (cx *cctx) indivTransform(prop string, cs []cv, k string) bool {
	idx := map[string]int{"translate": 0, "rotate": 1, "scale": 2}[prop]
	if k == "none" {
		cx.indiv[idx], cx.indivSet[idx] = nil, true
		return true
	}
	var t Transform
	switch prop {
	case "translate":
		if len(cs) > 3 {
			return false
		}
		var ls [2]Length
		for i := range cs {
			l, ok := cx.length(&cs[i], i < 2)
			if !ok {
				return false
			}
			if i < 2 {
				ls[i] = l
			}
		}
		t = Transform{Kind: TTranslate, X: ls[0], Y: ls[1]}
	case "scale":
		if len(cs) > 3 {
			return false
		}
		var s [2]float64
		for i := range cs {
			n, ok := cx.numPct(&cs[i])
			if !ok {
				return false
			}
			if i < 2 {
				s[i] = n
			}
		}
		if len(cs) == 1 {
			s[1] = s[0]
		}
		t = Transform{Kind: TScale, SX: float32(s[0]), SY: float32(s[1])}
	case "rotate":
		var a float64
		ok := false
		axisZ := true
		switch len(cs) {
		case 1:
			a, ok = cx.angle(&cs[0], false)
		case 2:
			ai, ci := 0, 1
			if cs[0].t.k != tIdent {
				ai, ci = 1, 0
			}
			a, ok = cx.angle(&cs[ci], false)
			switch kw(cs[ai : ai+1]) {
			case "z":
			case "x", "y":
				axisZ = false
			default:
				return false
			}
		case 4:
			var v [3]float64
			for i := 0; i < 3; i++ {
				n, nok := cx.number(&cs[i])
				if !nok {
					return false
				}
				v[i] = n
			}
			a, ok = cx.angle(&cs[3], false)
			axisZ = v[0] == 0 && v[1] == 0 && v[2] != 0
			if v[2] < 0 {
				a = -a
			}
		}
		if !ok {
			return false
		}
		if !axisZ {
			cx.indiv[idx], cx.indivSet[idx] = nil, true
			return true
		}
		t = Transform{Kind: TRotate, Angle: float32(a)}
	}
	cx.indiv[idx], cx.indivSet[idx] = &t, true
	return true
}

func (cx *cctx) origin(cs []cv) ([2]Length, bool) {
	if len(cs) == 0 || len(cs) > 3 {
		return [2]Length{}, false
	}
	pos := func(c *cv) (Length, byte, bool) {
		if c.t.k == tIdent {
			switch strings.ToLower(c.t.s) {
			case "left":
				return Pct(0), 'x', true
			case "right":
				return Pct(100), 'x', true
			case "top":
				return Pct(0), 'y', true
			case "bottom":
				return Pct(100), 'y', true
			case "center":
				return Pct(50), 'c', true
			}
			return Length{}, 0, false
		}
		l, ok := cx.length(c, true)
		return l, 'l', ok
	}
	out := [2]Length{Pct(50), Pct(50)}
	a, ka, ok := pos(&cs[0])
	if !ok {
		return out, false
	}
	if len(cs) == 1 {
		if ka == 'y' {
			out[1] = a
		} else {
			out[0] = a
		}
		return out, true
	}
	b, kb, ok := pos(&cs[1])
	if !ok {
		return out, false
	}
	if len(cs) == 3 {
		if _, ok := cx.length(&cs[2], false); !ok {
			return out, false
		}
	}
	if ka == 'y' || kb == 'x' {
		if ka == 'x' || kb == 'y' || ka == 'l' && kb == 'x' {
			return out, false
		}
		a, b = b, a
	}
	out[0], out[1] = a, b
	return out, true
}

// --- filters and shadows ---

var filterFns = map[string]bool{
	"blur": true, "brightness": true, "contrast": true, "drop-shadow": true, "grayscale": true,
	"hue-rotate": true, "invert": true, "opacity": true, "saturate": true, "sepia": true,
}

func (cx *cctx) filter(cs []cv) bool {
	f := Filter{Brightness: 1, Opacity: 1, Saturate: 1, Contrast: 1}
	var drops []Shadow
	for i := range cs {
		c := &cs[i]
		if c.t.k == tURL || c.isFn("url") {
			continue
		}
		if c.t.k != tFunction || !filterFns[c.name] {
			return false
		}
		amount := func(def float64) (float64, bool) {
			if len(c.args) == 0 {
				return def, true
			}
			if len(c.args) != 1 {
				return 0, false
			}
			v, ok := cx.numPct(&c.args[0])
			return v, ok && v >= 0
		}
		switch c.name {
		case "blur":
			if len(c.args) == 0 {
				continue
			}
			p, ok := cx.px(&c.args[0])
			if !ok || p < 0 || len(c.args) != 1 {
				return false
			}
			f.Blur += p
		case "grayscale", "invert", "sepia":
			v, ok := amount(1)
			if !ok {
				return false
			}
			v = clamp01(v)
			switch c.name {
			case "grayscale":
				f.Grayscale = float32(1 - (1-float64(f.Grayscale))*(1-v))
			case "invert":
				f.Invert = float32(v)
			default:
				f.Sepia = float32(1 - (1-float64(f.Sepia))*(1-v))
			}
		case "brightness", "opacity", "saturate", "contrast":
			v, ok := amount(1)
			if !ok {
				return false
			}
			switch c.name {
			case "brightness":
				f.Brightness *= float32(v)
			case "opacity":
				f.Opacity *= float32(clamp01(v))
			case "saturate":
				f.Saturate *= float32(v)
			default:
				f.Contrast *= float32(v)
			}
		case "hue-rotate":
			if len(c.args) == 0 {
				continue
			}
			a, ok := cx.angle(&c.args[0], false)
			if !ok {
				return false
			}
			f.HueRotate += float32(a)
		case "drop-shadow":
			sh, ok := cx.shadows(c.args, false)
			if !ok || len(sh) != 1 {
				return false
			}
			drops = append(drops, sh[0])
		}
	}
	// 0 means "unset" for the multiplicative ones: keep a real 0 visible
	const tiny = 1e-6
	for _, p := range []*float32{&f.Brightness, &f.Opacity, &f.Saturate, &f.Contrast} {
		if *p == 0 {
			*p = tiny
		}
	}
	cx.st.Filter = f
	cx.dropSh = drops
	return true
}

func (cx *cctx) shadows(cs []cv, box bool) ([]Shadow, bool) {
	var out []Shadow
	for _, layer := range splitComma(cs) {
		var sh Shadow
		var lens []float32
		haveColor := false
		sh.Color = cx.cur()
		lenDone := false
		for i := range layer {
			c := &layer[i]
			if box && c.t.ident("inset") {
				if sh.Inset {
					return nil, false
				}
				sh.Inset = true
				if len(lens) > 0 {
					lenDone = true
				}
				continue
			}
			if p, ok := cx.px(c); ok {
				if lenDone {
					return nil, false
				}
				lens = append(lens, p)
				continue
			}
			col, ok := cx.color(c)
			if !ok || haveColor {
				return nil, false
			}
			sh.Color, haveColor = col, true
			if len(lens) > 0 {
				lenDone = true
			}
		}
		max := 3
		if box {
			max = 4
		}
		if len(lens) < 2 || len(lens) > max {
			return nil, false
		}
		sh.X, sh.Y = lens[0], lens[1]
		if len(lens) > 2 {
			if lens[2] < 0 {
				return nil, false
			}
			sh.Blur = lens[2]
		}
		if len(lens) > 3 {
			sh.Spread = lens[3]
		}
		out = append(out, sh)
	}
	return out, true
}

// --- backgrounds ---

func (cx *cctx) image(c *cv) (bgLayer, bool) {
	switch {
	case c.t.ident("none"):
		return bgLayer{}, true
	case c.t.k == tURL:
		return bgLayer{url: c.t.s}, true
	case c.isFn("url") || c.isFn("src"):
		if len(c.args) >= 1 && c.args[0].t.k == tString {
			return bgLayer{url: c.args[0].t.s}, true
		}
		return bgLayer{}, false
	case c.t.k == tFunction:
		switch c.name {
		case "linear-gradient", "repeating-linear-gradient", "-webkit-linear-gradient", "-webkit-repeating-linear-gradient",
			"radial-gradient", "repeating-radial-gradient", "-webkit-radial-gradient", "-webkit-repeating-radial-gradient":
			g, ok := cx.gradient(c)
			return bgLayer{grad: g}, ok
		case "image-set", "-webkit-image-set":
			for _, part := range splitComma(c.args) {
				if len(part) > 0 {
					if part[0].t.k == tString {
						return bgLayer{url: part[0].t.s}, true
					}
					if l, ok := cx.image(&part[0]); ok {
						return l, true
					}
				}
			}
			return bgLayer{}, false
		case "conic-gradient", "repeating-conic-gradient", "cross-fade", "element", "-webkit-gradient", "paint", "image":
			return bgLayer{}, true // valid, not drawn
		}
	}
	return bgLayer{}, false
}

func (cx *cctx) gradient(c *cv) (*Gradient, bool) {
	g := &Gradient{Radial: strings.Contains(c.name, "radial"), Repeating: strings.Contains(c.name, "repeating"), Angle: math.Pi}
	legacy := strings.HasPrefix(c.name, "-webkit-")
	parts := splitComma(c.args)
	if len(parts) == 0 {
		return nil, false
	}
	first := parts[0]
	stopsFrom := 0
	if !g.Radial {
		if len(first) == 1 && first[0].t.k != tIdent && first[0].t.k != tHash && !(first[0].t.k == tFunction && !isMathFn(first[0].name)) {
			if a, ok := cx.angle(&first[0], false); ok {
				if legacy {
					a = math.Pi/2 - a
				}
				g.Angle = float32(a)
				stopsFrom = 1
			}
		} else if len(first) >= 1 && (first[0].t.ident("to") || legacy) {
			words := first
			if !legacy {
				words = first[1:]
			}
			var x, y float64
			valid := len(words) > 0
			for i := range words {
				switch strings.ToLower(words[i].t.s) {
				case "left":
					x = -1
				case "right":
					x = 1
				case "top":
					y = -1
				case "bottom":
					y = 1
				default:
					valid = false
				}
			}
			if valid {
				if legacy {
					x, y = -x, -y
				}
				g.Angle = float32(math.Atan2(x, -y))
				if g.Angle < 0 {
					g.Angle += 2 * math.Pi
				}
				stopsFrom = 1
			} else if !legacy {
				return nil, false
			}
		}
	} else {
		// shape / size / position prelude: anything that isn't a color stop
		if len(first) > 0 {
			if _, ok := cx.color(&first[0]); !ok {
				stopsFrom = 1
			}
		}
	}
	for _, part := range parts[stopsFrom:] {
		if len(part) == 0 {
			return nil, false
		}
		col, ok := cx.color(&part[0])
		if !ok {
			// a color hint (lone position): skip
			if len(part) == 1 {
				if _, ok := cx.length(&part[0], true); ok {
					continue
				}
			}
			return nil, false
		}
		var poss []float32
		for i := 1; i < len(part); i++ {
			p := &part[i]
			switch {
			case p.t.k == tPercentage:
				poss = append(poss, float32(p.t.num/100))
			default:
				if l, ok := cx.length(p, true); ok {
					if l.Px == 0 && l.Vw == 0 && l.Vh == 0 && l.x == nil {
						poss = append(poss, l.Pct/100)
					} else {
						poss = append(poss, -1)
					}
					continue
				}
				return nil, false
			}
		}
		if len(poss) > 2 {
			return nil, false
		}
		if len(poss) == 0 {
			poss = []float32{-1}
		}
		for _, p := range poss {
			g.Stops = append(g.Stops, Stop{Color: col, Pos: p})
		}
	}
	if len(g.Stops) < 1 {
		return nil, false
	}
	return g, true
}

func (cx *cctx) bgSizeText(src string, cs []cv) (string, bool) {
	switch kw(cs) {
	case "cover", "contain", "auto":
		return kw(cs), true
	}
	if len(cs) == 0 || len(cs) > 2 {
		return "", false
	}
	for i := range cs {
		if cs[i].t.ident("auto") {
			continue
		}
		l, ok := cx.length(&cs[i], true)
		if !ok || negLen(&cs[i], l) {
			return "", false
		}
	}
	return strings.ToLower(spanText(src, cs)), true
}

func (cx *cctx) validPosition(cs []cv) bool {
	if len(cs) == 0 || len(cs) > 4 {
		return false
	}
	for i := range cs {
		c := &cs[i]
		if c.t.k == tIdent {
			switch strings.ToLower(c.t.s) {
			case "left", "right", "top", "bottom", "center", "x-start", "x-end", "y-start", "y-end":
				continue
			}
			return false
		}
		if _, ok := cx.length(c, true); !ok {
			return false
		}
	}
	return true
}

func bgRepeat(cs []cv) (bool, bool) {
	if len(cs) == 0 || len(cs) > 2 {
		return false, false
	}
	rep := false
	for i := range cs {
		switch strings.ToLower(cs[i].t.s) {
		case "repeat", "space", "round", "repeat-x", "repeat-y":
			rep = true
		case "no-repeat":
		default:
			return false, false
		}
		if cs[i].t.k != tIdent {
			return false, false
		}
	}
	return rep, true
}

var blendModes = map[string]bool{
	"normal": true, "multiply": true, "screen": true, "overlay": true, "darken": true, "lighten": true,
	"color-dodge": true, "color-burn": true, "hard-light": true, "soft-light": true, "difference": true,
	"exclusion": true, "hue": true, "saturation": true, "color": true, "luminosity": true,
	"plus-darker": true, "plus-lighter": true,
}

// --- fonts ---

func fontFamily(cs []cv, src string) (string, bool) {
	var names []string
	for _, part := range splitComma(cs) {
		if len(part) == 0 {
			return "", false
		}
		if len(part) == 1 && part[0].t.k == tString {
			names = append(names, part[0].t.s)
			continue
		}
		var w []string
		for i := range part {
			if part[i].t.k != tIdent {
				return "", false
			}
			w = append(w, part[i].t.s)
		}
		names = append(names, strings.Join(w, " "))
	}
	return strings.Join(names, ", "), len(names) > 0
}

var fontSizeKw = map[string]float32{
	"xx-small": 9, "x-small": 10, "small": 13, "medium": 16, "large": 18, "x-large": 24,
	"xx-large": 32, "xxx-large": 48,
}

func (cx *cctx) fontSize(cs []cv, k string) bool {
	pfs := float32(16)
	if cx.parent != nil {
		pfs = cx.parent.FontSize
	}
	if v, ok := fontSizeKw[k]; ok {
		cx.st.FontSize = v
		return true
	}
	switch k {
	case "smaller":
		cx.st.FontSize = pfs / 1.2
		return true
	case "larger":
		cx.st.FontSize = pfs * 1.2
		return true
	case "math":
		cx.st.FontSize = pfs
		return true
	}
	if len(cs) != 1 {
		return false
	}
	l, ok := cx.length(&cs[0], true)
	if !ok {
		return false
	}
	v := l.Resolve(pfs, cx.env.Width, cx.env.Height, pfs)
	if negLen(&cs[0], l) {
		return false
	}
	cx.st.FontSize = max(v, 0)
	return true
}

func (cx *cctx) fontWeight(cs []cv, k string) (int, bool) {
	pw := 400
	if cx.parent != nil {
		pw = cx.parent.FontWeight
	}
	switch k {
	case "normal":
		return 400, true
	case "bold":
		return 700, true
	case "bolder":
		switch {
		case pw < 350:
			return 400, true
		case pw < 550:
			return 700, true
		case pw < 900:
			return 900, true
		}
		return pw, true
	case "lighter":
		switch {
		case pw < 100:
			return pw, true
		case pw < 550:
			return 100, true
		case pw < 750:
			return 400, true
		}
		return 700, true
	}
	if len(cs) != 1 {
		return 0, false
	}
	n, ok := cx.number(&cs[0])
	if !ok || n < 1 || n > 1000 {
		return 0, false
	}
	return int(math.Round(n)), true
}

// --- misc ---

var cursorKw = map[string]Cursor{
	"auto": CursorAuto, "default": CursorDefault, "pointer": CursorPointer, "hand": CursorPointer,
	"text": CursorText, "vertical-text": CursorText, "move": CursorMove, "all-scroll": CursorMove,
	"grab": CursorGrab, "-webkit-grab": CursorGrab, "-moz-grab": CursorGrab,
	"grabbing": CursorGrabbing, "-webkit-grabbing": CursorGrabbing, "-moz-grabbing": CursorGrabbing,
	"not-allowed": CursorNotAllowed, "no-drop": CursorNotAllowed, "wait": CursorWait,
	"progress": CursorProgress, "crosshair": CursorCrosshair, "cell": CursorCrosshair,
	"help": CursorHelp, "none": CursorNone, "col-resize": CursorColResize, "row-resize": CursorRowResize,
	"ew-resize": CursorEWResize, "e-resize": CursorEWResize, "w-resize": CursorEWResize,
	"ns-resize": CursorNSResize, "n-resize": CursorNSResize, "s-resize": CursorNSResize,
	"nesw-resize": CursorNESWResize, "ne-resize": CursorNESWResize, "sw-resize": CursorNESWResize,
	"nwse-resize": CursorNWSEResize, "nw-resize": CursorNWSEResize, "se-resize": CursorNWSEResize,
	"context-menu": CursorDefault, "alias": CursorDefault, "copy": CursorDefault,
	"zoom-in": CursorPointer, "zoom-out": CursorPointer, "-webkit-zoom-in": CursorPointer, "-webkit-zoom-out": CursorPointer,
}

func (cx *cctx) cursor(cs []cv) bool {
	parts := splitComma(cs)
	last := parts[len(parts)-1]
	for _, p := range parts[:len(parts)-1] {
		if len(p) == 0 || !(p[0].t.k == tURL || p[0].isFn("url") || p[0].isFn("image-set") || p[0].isFn("-webkit-image-set")) {
			return false
		}
	}
	k := kw(last)
	c, ok := cursorKw[k]
	if !ok {
		return false
	}
	cx.st.Cursor = c
	return true
}

func (cx *cctx) content(cs []cv, k string) bool {
	st := cx.st
	switch k {
	case "none", "normal":
		st.Content, st.HasContent = "", false
		return true
	}
	var b strings.Builder
	for i := range cs {
		c := &cs[i]
		switch {
		case c.t.is('/'):
			goto done // alt text follows
		case c.t.k == tString:
			b.WriteString(c.t.s)
		case c.t.k == tURL || c.isFn("url") || c.isFn("image-set") || strings.HasSuffix(c.name, "gradient"):
		case c.isFn("attr"):
			// resolved at cascade time when the element is known
			if len(c.args) == 0 || c.args[0].t.k != tIdent {
				return false
			}
		case c.isFn("counter") || c.isFn("counters"):
		case c.t.k == tIdent:
			switch strings.ToLower(c.t.s) {
			case "open-quote":
				b.WriteString("“")
			case "close-quote":
				b.WriteString("”")
			case "no-open-quote", "no-close-quote":
			default:
				return false
			}
		default:
			return false
		}
	}
done:
	st.Content, st.HasContent = b.String(), true
	return true
}

func (cx *cctx) paint(cs []cv, k string) (color.NRGBA, bool, bool) {
	switch k {
	case "none":
		return color.NRGBA{}, true, true
	case "context-fill", "context-stroke":
		return cx.cur(), false, true
	}
	if cs[0].t.k == tURL || cs[0].isFn("url") {
		if len(cs) == 1 {
			return color.NRGBA{}, true, true
		}
		if len(cs) == 2 {
			if cs[1].t.ident("none") {
				return color.NRGBA{}, true, true
			}
			c, ok := cx.color(&cs[1])
			return c, false, ok
		}
		return color.NRGBA{}, false, false
	}
	if len(cs) != 1 {
		return color.NRGBA{}, false, false
	}
	c, ok := cx.color(&cs[0])
	return c, false, ok
}

// acceptedProps are valid properties Clack doesn't draw: they parse (for
// @supports and the cascade) but compute to nothing.
var acceptedProps = map[string]bool{}

func init() {
	for _, p := range strings.Fields(`
transition transition-property transition-duration transition-timing-function transition-delay transition-behavior
animation animation-name animation-duration animation-timing-function animation-delay animation-iteration-count
animation-direction animation-fill-mode animation-play-state animation-composition animation-timeline animation-range
will-change contain content-visibility contain-intrinsic-size contain-intrinsic-width contain-intrinsic-height
container container-name container-type
scrollbar-width scrollbar-color scrollbar-gutter scroll-behavior scroll-snap-type scroll-snap-align scroll-snap-stop
scroll-margin scroll-margin-top scroll-margin-right scroll-margin-bottom scroll-margin-left scroll-margin-inline scroll-margin-block
scroll-padding scroll-padding-top scroll-padding-right scroll-padding-bottom scroll-padding-left scroll-padding-inline scroll-padding-block
scroll-timeline view-timeline overscroll-behavior overscroll-behavior-x overscroll-behavior-y overscroll-behavior-inline overscroll-behavior-block
font-variant font-variant-ligatures font-variant-caps font-variant-numeric font-variant-east-asian font-variant-alternates font-variant-position
font-feature-settings font-variation-settings font-stretch font-width font-kerning font-optical-sizing font-synthesis
font-synthesis-weight font-synthesis-style font-synthesis-small-caps font-size-adjust font-palette font-language-override
text-rendering -webkit-font-smoothing -moz-osx-font-smoothing text-size-adjust -webkit-text-size-adjust -moz-text-size-adjust
list-style list-style-type list-style-position list-style-image table-layout border-collapse border-spacing caption-side empty-cells
resize appearance caret-color accent-color clip clip-path clip-rule mask mask-image mask-size mask-position mask-repeat
mask-mode mask-origin mask-clip mask-composite mask-type -webkit-mask -webkit-mask-image -webkit-mask-size -webkit-mask-position
-webkit-mask-repeat -webkit-mask-composite -webkit-mask-box-image isolation backface-visibility perspective perspective-origin
transform-style transform-box direction unicode-bidi writing-mode text-orientation hyphens tab-size text-indent
text-underline-offset text-underline-position text-decoration-thickness text-decoration-skip-ink
text-decoration-skip text-emphasis text-emphasis-style text-emphasis-color text-emphasis-position text-justify text-align-last
quotes counter-reset counter-increment counter-set orphans widows page-break-before page-break-after page-break-inside
break-before break-after break-inside columns column-count column-width column-rule column-rule-width column-rule-style
column-rule-color column-span column-fill image-rendering image-orientation image-resolution shape-outside shape-margin
shape-image-threshold touch-action float clear color-scheme forced-color-adjust print-color-adjust -webkit-print-color-adjust
-webkit-tap-highlight-color -webkit-touch-callout -webkit-text-fill-color -webkit-text-stroke -webkit-text-stroke-width
-webkit-text-stroke-color -webkit-user-drag -webkit-app-region -webkit-overflow-scrolling -webkit-user-modify -webkit-locale
-webkit-box-decoration-break box-decoration-break -webkit-font-feature-settings -webkit-highlight -webkit-rtl-ordering
-webkit-box-reflect -webkit-border-image -webkit-margin-before -webkit-margin-after -webkit-margin-start -webkit-margin-end
-webkit-padding-start -webkit-padding-end -webkit-column-break-inside -webkit-hyphens -webkit-text-security
-webkit-box-ordinal-group -webkit-box-lines -webkit-tap-highlight -webkit-text-emphasis -webkit-writing-mode
zoom field-sizing anchor-name anchor-scope position-anchor position-area inset-area position-try position-try-fallbacks
position-try-order position-visibility view-transition-name view-transition-class interpolate-size text-wrap-style
hanging-punctuation line-break white-space-collapse text-spacing-trim text-autospace word-wrap
background-attachment background-origin background-clip background-position-x background-position-y background-blend-mode
object-position object-view-box offset offset-path offset-distance offset-rotate offset-anchor offset-position
paint-order stroke-linecap stroke-linejoin stroke-dasharray stroke-dashoffset stroke-miterlimit stroke-opacity
fill-opacity fill-rule stop-color stop-opacity flood-color flood-opacity lighting-color marker marker-start marker-mid
marker-end color-interpolation color-interpolation-filters color-rendering shape-rendering text-anchor dominant-baseline
alignment-baseline baseline-shift vector-effect cx cy r rx ry x y d
grid-template-rows grid-auto-rows grid-auto-columns grid-auto-flow grid-row grid-row-start grid-row-end
grid-column grid-column-start grid-column-end grid-area
border-image border-image-source border-image-slice border-image-width border-image-outset border-image-repeat
outline-offset-x box-flex box-orient box-pack box-align ruby-position ruby-align math-style math-depth math-shift
initial-letter text-box text-box-trim text-box-edge dynamic-range-limit reading-flow reading-order
`) {
		acceptedProps[p] = true
	}
}
